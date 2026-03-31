//go:build integration

package bridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	servicev1 "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	"github.com/openmanet/openmanetd/internal/api/openmanet/service/v1/servicev1connect"
	ws "github.com/openmanet/openmanetd/internal/websocket"
	"google.golang.org/protobuf/types/known/emptypb"
)

// integrationCommsHandler tracks RPC calls for integration testing.
type integrationCommsHandler struct {
	servicev1connect.UnimplementedCommsServiceHandler
	sendCalls    chan *servicev1.SetSendTalkGroupRequest
	receiveCalls chan *servicev1.SetReceiveTalkGroupRequest
}

func (h *integrationCommsHandler) GetCommsStatus(_ context.Context, _ *emptypb.Empty) (*servicev1.GetCommsStatusResponse, error) {
	return &servicev1.GetCommsStatusResponse{
		ActiveTalkgroup:     1,
		AvailableTalkgroups: []int32{1, 2, 3, 4, 5},
	}, nil
}

func (h *integrationCommsHandler) SetSendTalkGroup(_ context.Context, req *servicev1.SetSendTalkGroupRequest) (*servicev1.SetSendTalkGroupResponse, error) {
	h.sendCalls <- req
	return &servicev1.SetSendTalkGroupResponse{Success: true}, nil
}

func (h *integrationCommsHandler) SetReceiveTalkGroup(_ context.Context, req *servicev1.SetReceiveTalkGroupRequest) (*servicev1.SetReceiveTalkGroupResponse, error) {
	h.receiveCalls <- req
	return &servicev1.SetReceiveTalkGroupResponse{Success: true}, nil
}

// integrationWebCommsHandler tracks PTT calls.
type integrationWebCommsHandler struct {
	servicev1connect.UnimplementedWebCommsServiceHandler
	pttCalls chan *servicev1.SendPTTEventRequest
}

func (h *integrationWebCommsHandler) SendPTTEvent(_ context.Context, req *servicev1.SendPTTEventRequest) (*servicev1.SendPTTEventResponse, error) {
	h.pttCalls <- req
	return &servicev1.SendPTTEventResponse{Success: true}, nil
}

func TestIntegration_WSClientToRPC(t *testing.T) {
	// Stand up mock Connect RPC server.
	commsHandler := &integrationCommsHandler{
		sendCalls:    make(chan *servicev1.SetSendTalkGroupRequest, 10),
		receiveCalls: make(chan *servicev1.SetReceiveTalkGroupRequest, 10),
	}
	webCommsHandler := &integrationWebCommsHandler{
		pttCalls: make(chan *servicev1.SendPTTEventRequest, 10),
	}

	mux := http.NewServeMux()
	commsPath, commsH := servicev1connect.NewCommsServiceHandler(commsHandler)
	mux.Handle(commsPath, commsH)
	webCommsPath, webCommsH := servicev1connect.NewWebCommsServiceHandler(webCommsHandler)
	mux.Handle(webCommsPath, webCommsH)

	rpcServer := httptest.NewServer(mux)
	defer rpcServer.Close()

	// Create RPC client pointing to mock server.
	rpcClient := openmanetd.NewClient(rpcServer.URL, rpcServer.Client())

	// Create the bridge + hub.
	var b *Bridge
	hub := ws.NewHub(func(client *ws.Client, data []byte) {
		b.HandleMessage(client, data)
	})
	b = NewBridge(hub, rpcClient, rpcClient)
	go hub.Run(context.Background())

	// Create WS server with the hub.
	wsMux := http.NewServeMux()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	wsMux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}
		client := ws.NewClient(hub, conn)
		hub.Register(client)
		go client.WritePump()
		go client.ReadPump()
	})
	wsServer := httptest.NewServer(wsMux)
	defer wsServer.Close()

	// Connect a WebSocket client.
	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WS dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond) // Let registration complete.

	// Send a TX toggle message through the WebSocket.
	txToggle := []byte{ws.OpcodeTXToggle, 3, 1} // channel 3, on
	if err := conn.WriteMessage(websocket.BinaryMessage, txToggle); err != nil {
		t.Fatalf("WS write: %v", err)
	}

	// Verify the RPC was called.
	select {
	case req := <-commsHandler.sendCalls:
		if req.Talkgroup != 3 {
			t.Errorf("talkgroup = %d, want 3", req.Talkgroup)
		}
		if !req.Enabled {
			t.Error("enabled = false, want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SetSendTalkGroup RPC")
	}

	// Send an RX toggle message.
	rxToggle := []byte{ws.OpcodeRXToggle, 1, 0} // channel 1, off
	if err := conn.WriteMessage(websocket.BinaryMessage, rxToggle); err != nil {
		t.Fatalf("WS write: %v", err)
	}

	select {
	case req := <-commsHandler.receiveCalls:
		if req.Talkgroup != 1 {
			t.Errorf("talkgroup = %d, want 1", req.Talkgroup)
		}
		if req.Enabled {
			t.Error("enabled = true, want false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SetReceiveTalkGroup RPC")
	}
}
