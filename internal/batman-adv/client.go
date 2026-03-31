package batmanadv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"github.com/rs/zerolog"
)

// Querier abstracts genetlink query execution for testability.
type Querier interface {
	Execute(msg genetlink.Message, family uint16, flags netlink.HeaderFlags) ([]genetlink.Message, error)
	Close() error
}

// genetlinkQuerier wraps a real genetlink.Conn to satisfy the Querier interface.
type genetlinkQuerier struct {
	conn *genetlink.Conn
}

func (q *genetlinkQuerier) Execute(msg genetlink.Message, family uint16, flags netlink.HeaderFlags) ([]genetlink.Message, error) {
	return q.conn.Execute(msg, family, flags) //nolint:wrapcheck // thin wrapper, callers handle errors
}

func (q *genetlinkQuerier) Close() error {
	return q.conn.Close() //nolint:wrapcheck // thin wrapper
}

// Client provides netlink-based access to batman-adv mesh data.
// It falls back to batctl CLI commands when the batman-adv generic netlink
// family is unavailable (e.g., module not loaded).
type Client struct {
	logger       zerolog.Logger
	querier      Querier
	ctx          context.Context //nolint:containedctx // lifecycle pattern matching blos/status_worker.go
	listener     *Listener
	cancel       context.CancelFunc
	meshConfig   *MeshConfig
	iface        string
	family       genetlink.Family
	meshIfindex  int
	cacheMu      sync.RWMutex
	queryMu      sync.Mutex
	useBatctl    atomic.Bool
	reconnecting atomic.Bool
	closed       atomic.Bool
	configValid  bool
}

// NewClient creates a new batman-adv netlink client for the given mesh interface.
// If the batman-adv genl family is not available, the client operates in fallback
// mode using batctl CLI commands.
func NewClient(iface string, logger zerolog.Logger) (*Client, error) {
	ctx, cancel := context.WithCancel(context.Background())

	c := &Client{
		iface:  iface,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}

	if err := c.connect(); err != nil {
		// If connection fails, operate in fallback mode
		c.useBatctl.Store(true)
		c.logger.Warn().Err(err).Msg("batman-adv netlink unavailable, using batctl fallback")

		return c, nil
	}

	return c, nil
}

// newClientWithQuerier creates a client with an injected Querier for testing.
func newClientWithQuerier(querier Querier, family genetlink.Family, iface string, ifindex int, logger zerolog.Logger) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		querier:     querier,
		family:      family,
		iface:       iface,
		meshIfindex: ifindex,
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// connect establishes the genetlink connection and resolves the batman-adv family.
func (c *Client) connect() error {
	conn, err := genetlink.Dial(nil)
	if err != nil {
		return fmt.Errorf("genetlink dial: %w", err)
	}

	family, err := conn.GetFamily(BatadvNLName)
	if err != nil {
		conn.Close()

		return fmt.Errorf("get batadv family: %w", err)
	}

	ifindex, err := resolveIfindex(c.iface)
	if err != nil {
		conn.Close()

		return fmt.Errorf("resolve interface %s: %w", c.iface, err)
	}

	c.querier = &genetlinkQuerier{conn: conn}
	c.family = family
	c.meshIfindex = ifindex

	return nil
}

// resolveIfindex resolves a network interface name to its index.
func resolveIfindex(name string) (int, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return 0, fmt.Errorf("interface %s: %w", name, err)
	}

	return iface.Index, nil
}

// GetMeshConfig queries the mesh configuration via netlink.
// Returns a cached result if the cache is valid (invalidated by event listener).
func (c *Client) GetMeshConfig() (*MeshConfig, error) {
	if c.closed.Load() {
		return nil, errors.New("client closed")
	}

	if c.useBatctl.Load() {
		return GetMeshConfigBatctl(c.iface)
	}

	// Check cache
	c.cacheMu.RLock()

	if c.configValid && c.meshConfig != nil {
		cfg := *c.meshConfig
		c.cacheMu.RUnlock()

		return &cfg, nil
	}

	c.cacheMu.RUnlock()

	// Query via netlink
	cfg, err := c.queryMeshConfig()
	if err != nil {
		if c.isConnectionLost(err) {
			c.handleConnectionLoss()

			return GetMeshConfigBatctl(c.iface)
		}

		return nil, err
	}

	// Update cache
	c.cacheMu.Lock()
	c.meshConfig = cfg
	c.configValid = true
	c.cacheMu.Unlock()

	return cfg, nil
}

// GetMeshGateways queries the gateway list via netlink.
func (c *Client) GetMeshGateways() (*Gateways, error) {
	if c.closed.Load() {
		return nil, errors.New("client closed")
	}

	if c.useBatctl.Load() {
		return GetMeshGatewaysBatctl(c.iface)
	}

	gws, err := c.queryGateways()
	if err != nil {
		if c.isConnectionLost(err) {
			c.handleConnectionLoss()

			return GetMeshGatewaysBatctl(c.iface)
		}

		return nil, err
	}

	return gws, nil
}

// GetMeshNeighbors queries the neighbor list via netlink.
func (c *Client) GetMeshNeighbors() (*Neighbors, error) {
	if c.closed.Load() {
		return nil, errors.New("client closed")
	}

	if c.useBatctl.Load() {
		return GetMeshNeighborsBatctl()
	}

	ns, err := c.queryNeighbors()
	if err != nil {
		if c.isConnectionLost(err) {
			c.handleConnectionLoss()

			return GetMeshNeighborsBatctl()
		}

		return nil, err
	}

	return ns, nil
}

// GetOriginators queries the originator list via netlink.
// This method satisfies the OriginatorProvider interface.
func (c *Client) GetOriginators() ([]Originator, error) {
	if c.closed.Load() {
		return nil, errors.New("client closed")
	}

	if c.useBatctl.Load() {
		p := &BatctlOriginatorProvider{}

		return p.GetOriginators()
	}

	origs, err := c.queryOriginators()
	if err != nil {
		if c.isConnectionLost(err) {
			c.handleConnectionLoss()

			p := &BatctlOriginatorProvider{}

			return p.GetOriginators()
		}

		return nil, err
	}

	return origs, nil
}

// InvalidateCache marks the MeshConfig cache as stale.
func (c *Client) InvalidateCache() {
	c.cacheMu.Lock()
	c.configValid = false
	c.cacheMu.Unlock()
}

// StartEventListener starts listening for batman-adv netlink multicast events.
// Events trigger cache invalidation for MeshConfig.
func (c *Client) StartEventListener(ctx context.Context) error {
	if c.useBatctl.Load() {
		return nil
	}

	l, err := NewListener(c.family, c.logger)
	if err != nil {
		return fmt.Errorf("create listener: %w", err)
	}

	c.listener = l
	l.SetOnMeshConfigChange(c.InvalidateCache)
	l.Start(ctx)

	return nil
}

// IsFallbackMode returns true if the client is using batctl CLI fallback.
func (c *Client) IsFallbackMode() bool {
	return c.useBatctl.Load()
}

// Close releases all resources held by the client.
func (c *Client) Close() error {
	if c.closed.Swap(true) {
		return nil // already closed
	}

	c.cancel()

	if c.listener != nil {
		c.listener.Stop()
	}

	if c.querier != nil {
		return c.querier.Close()
	}

	return nil
}

// queryMeshConfig sends a BATADV_CMD_GET_MESH query and parses the response.
func (c *Client) queryMeshConfig() (*MeshConfig, error) {
	ae := netlink.NewAttributeEncoder()
	ae.Uint32(BatadvAttrMeshIfindex, uint32(c.meshIfindex))

	attrData, err := ae.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode attrs: %w", err)
	}

	msg := genetlink.Message{
		Header: genetlink.Header{
			Command: BatadvCmdGetMesh,
			Version: 1,
		},
		Data: attrData,
	}

	c.queryMu.Lock()
	msgs, err := c.querier.Execute(msg, c.family.ID, 0)
	c.queryMu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("execute get_mesh: %w", err)
	}

	if len(msgs) == 0 {
		return nil, errors.New("get_mesh: empty response")
	}

	attrs, err := netlink.UnmarshalAttributes(msgs[0].Data)
	if err != nil {
		return nil, fmt.Errorf("unmarshal attrs: %w", err)
	}

	return parseMeshConfig(attrs)
}

// queryGateways sends a BATADV_CMD_GET_GATEWAYS dump query.
func (c *Client) queryGateways() (*Gateways, error) {
	ae := netlink.NewAttributeEncoder()
	ae.Uint32(BatadvAttrMeshIfindex, uint32(c.meshIfindex))

	attrData, err := ae.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode attrs: %w", err)
	}

	msg := genetlink.Message{
		Header: genetlink.Header{
			Command: BatadvCmdGetGateways,
			Version: 1,
		},
		Data: attrData,
	}

	c.queryMu.Lock()
	msgs, err := c.querier.Execute(msg, c.family.ID, netlink.Request|netlink.Dump)
	c.queryMu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("execute get_gateways: %w", err)
	}

	gws := make(Gateways, 0, len(msgs))

	for _, m := range msgs {
		attrs, err := netlink.UnmarshalAttributes(m.Data)
		if err != nil {
			c.logger.Warn().Err(err).Msg("skip gateway entry: unmarshal error")

			continue
		}

		gw, err := parseGateway(attrs)
		if err != nil {
			c.logger.Warn().Err(err).Msg("skip gateway entry: parse error")

			continue
		}

		gws = append(gws, *gw)
	}

	return &gws, nil
}

// queryNeighbors sends a BATADV_CMD_GET_NEIGHBORS dump query.
func (c *Client) queryNeighbors() (*Neighbors, error) {
	ae := netlink.NewAttributeEncoder()
	ae.Uint32(BatadvAttrMeshIfindex, uint32(c.meshIfindex))

	attrData, err := ae.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode attrs: %w", err)
	}

	msg := genetlink.Message{
		Header: genetlink.Header{
			Command: BatadvCmdGetNeighbors,
			Version: 1,
		},
		Data: attrData,
	}

	c.queryMu.Lock()
	msgs, err := c.querier.Execute(msg, c.family.ID, netlink.Request|netlink.Dump)
	c.queryMu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("execute get_neighbors: %w", err)
	}

	ns := make(Neighbors, 0, len(msgs))

	for _, m := range msgs {
		attrs, err := netlink.UnmarshalAttributes(m.Data)
		if err != nil {
			c.logger.Warn().Err(err).Msg("skip neighbor entry: unmarshal error")

			continue
		}

		n, err := parseNeighbor(attrs)
		if err != nil {
			c.logger.Warn().Err(err).Msg("skip neighbor entry: parse error")

			continue
		}

		ns = append(ns, *n)
	}

	return &ns, nil
}

// queryOriginators sends a BATADV_CMD_GET_ORIGINATORS dump query.
func (c *Client) queryOriginators() ([]Originator, error) {
	ae := netlink.NewAttributeEncoder()
	ae.Uint32(BatadvAttrMeshIfindex, uint32(c.meshIfindex))

	attrData, err := ae.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode attrs: %w", err)
	}

	msg := genetlink.Message{
		Header: genetlink.Header{
			Command: BatadvCmdGetOriginators,
			Version: 1,
		},
		Data: attrData,
	}

	c.queryMu.Lock()
	msgs, err := c.querier.Execute(msg, c.family.ID, netlink.Request|netlink.Dump)
	c.queryMu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("execute get_originators: %w", err)
	}

	origs := make([]Originator, 0, len(msgs))

	for _, m := range msgs {
		attrs, err := netlink.UnmarshalAttributes(m.Data)
		if err != nil {
			c.logger.Warn().Err(err).Msg("skip originator entry: unmarshal error")

			continue
		}

		o, err := parseOriginator(attrs)
		if err != nil {
			c.logger.Warn().Err(err).Msg("skip originator entry: parse error")

			continue
		}

		origs = append(origs, *o)
	}

	return origs, nil
}

// isConnectionLost checks if an error indicates the netlink connection is broken.
func (c *Client) isConnectionLost(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ENODEV) {
		return true
	}

	// Also check for generic "closed" errors
	var opErr *net.OpError

	return errors.As(err, &opErr)
}

// handleConnectionLoss transitions to fallback mode and starts reconnection.
func (c *Client) handleConnectionLoss() {
	c.useBatctl.Store(true)

	c.cacheMu.Lock()
	c.configValid = false
	c.cacheMu.Unlock()

	c.logger.Warn().Msg("batman-adv netlink connection lost, falling back to batctl")

	// Start reconnect if not already in progress
	if c.reconnecting.CompareAndSwap(false, true) {
		go c.reconnectLoop()
	}
}

// reconnectLoop attempts to re-establish the netlink connection with exponential backoff.
func (c *Client) reconnectLoop() {
	defer c.reconnecting.Store(false)

	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Close old querier
		if c.querier != nil {
			c.querier.Close()
			c.querier = nil
		}

		if err := c.connect(); err != nil {
			c.logger.Debug().Err(err).Dur("next_retry", backoff).Msg("batman-adv reconnect failed")

			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}

			continue
		}

		c.useBatctl.Store(false)
		c.logger.Info().Msg("batman-adv netlink connection restored")

		// Restart event listener if it was previously running
		if c.listener != nil {
			c.listener.Stop()

			if err := c.StartEventListener(c.ctx); err != nil {
				c.logger.Warn().Err(err).Msg("failed to restart event listener after reconnect")
			}
		}

		return
	}
}

// defaultClient is the package-level client set during initialization.
// It is written once by SetDefaultClient before any workers start,
// and read by the free functions (GetMeshConfig, etc.).
var defaultClient atomic.Pointer[Client] //nolint:gochecknoglobals // package-level singleton by design

// SetDefaultClient sets the package-level default client used by the
// free functions (GetMeshConfig, GetMeshGateways, GetMeshNeighbors).
// Must be called before any workers start.
func SetDefaultClient(c *Client) {
	defaultClient.Store(c)
}

// getDefaultClient returns the current default client, or nil.
func getDefaultClient() *Client {
	return defaultClient.Load()
}

// Compile-time check: *Client satisfies OriginatorProvider.
var _ OriginatorProvider = (*Client)(nil)
