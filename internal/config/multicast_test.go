package config

import (
	"slices"
	"testing"
)

func TestGetMulticastGroupAddresses(t *testing.T) {
	result := GetMulticastGroupAddresses()

	// Check that result contains all expected addresses
	expectedAddresses := []string{
		ATAKSAAddress,
		ATAKChatAddress,
		MDNSAddress,
		TalkGroupMcastAddr,
	}

	if len(result) != len(expectedAddresses) {
		t.Errorf("expected %d addresses, got %d", len(expectedAddresses), len(result))
	}

	for _, addr := range expectedAddresses {
		if !slices.Contains(result, addr) {
			t.Errorf("expected address %s not found in result", addr)
		}
	}
}

func TestGetMulticastGroupAddressesLength(t *testing.T) {
	result := GetMulticastGroupAddresses()
	expected := len(multicastGroupAddresses)

	if len(result) != expected {
		t.Errorf("expected length %d, got %d", expected, len(result))
	}
}

func TestGetMulticastGroupAddressesNotNil(t *testing.T) {
	result := GetMulticastGroupAddresses()

	if result == nil {
		t.Error("expected non-nil result, got nil")
	}
}
func TestGetMulticastGroupSet(t *testing.T) {
	result := GetMulticastGroupSet()

	expectedAddresses := []string{
		ATAKSAAddress,
		ATAKChatAddress,
		MDNSAddress,
		TalkGroupMcastAddr,
	}

	if len(result) != len(expectedAddresses) {
		t.Errorf("expected %d addresses in set, got %d", len(expectedAddresses), len(result))
	}

	for _, addr := range expectedAddresses {
		if !result[addr] {
			t.Errorf("expected address %s not found in result set", addr)
		}
	}
}

func TestGetMulticastGroupSetNotNil(t *testing.T) {
	result := GetMulticastGroupSet()

	if result == nil {
		t.Error("expected non-nil result, got nil")
	}
}

func TestGetMulticastGroupSetValues(t *testing.T) {
	result := GetMulticastGroupSet()

	for addr, value := range result {
		if !value {
			t.Errorf("expected address %s to have value true, got false", addr)
		}
	}
}

func TestGetMulticastGroupAddressesImmutability(t *testing.T) {
	result1 := GetMulticastGroupAddresses()
	result1[0] = "1.2.3.4"
	result2 := GetMulticastGroupAddresses()

	if result2[0] == "1.2.3.4" {
		t.Error("modifying returned slice should not affect subsequent calls")
	}
}

func TestGetMulticastGroupSetImmutability(t *testing.T) {
	result1 := GetMulticastGroupSet()
	result1["1.2.3.4"] = true
	result2 := GetMulticastGroupSet()

	if result2["1.2.3.4"] {
		t.Error("modifying returned map should not affect subsequent calls")
	}
}

func TestGetMulticastGroupSetSize(t *testing.T) {
	result := GetMulticastGroupSet()
	expected := len(multicastGroupSet)

	if len(result) != expected {
		t.Errorf("expected set size %d, got %d", expected, len(result))
	}
}

func TestGetMulticastGroupAddressesContainsATAKSA(t *testing.T) {
	result := GetMulticastGroupAddresses()

	if !slices.Contains(result, ATAKSAAddress) {
		t.Errorf("expected ATAKSAAddress %s in result", ATAKSAAddress)
	}
}

func TestGetMulticastGroupAddressesContainsATAKChat(t *testing.T) {
	result := GetMulticastGroupAddresses()

	if !slices.Contains(result, ATAKChatAddress) {
		t.Errorf("expected ATAKChatAddress %s in result", ATAKChatAddress)
	}
}

func TestGetMulticastGroupAddressesContainsMDNS(t *testing.T) {
	result := GetMulticastGroupAddresses()

	if !slices.Contains(result, MDNSAddress) {
		t.Errorf("expected MDNSAddress %s in result", MDNSAddress)
	}
}

func TestGetMulticastGroupSetContainsATAKSA(t *testing.T) {
	result := GetMulticastGroupSet()

	if !result[ATAKSAAddress] {
		t.Errorf("expected ATAKSAAddress %s in result set", ATAKSAAddress)
	}
}

func TestGetMulticastGroupSetContainsATAKChat(t *testing.T) {
	result := GetMulticastGroupSet()

	if !result[ATAKChatAddress] {
		t.Errorf("expected ATAKChatAddress %s in result set", ATAKChatAddress)
	}
}

func TestGetMulticastGroupSetContainsMDNS(t *testing.T) {
	result := GetMulticastGroupSet()

	if !result[MDNSAddress] {
		t.Errorf("expected MDNSAddress %s in result set", MDNSAddress)
	}
}

func TestGetMulticastGroupSetContainsTalkGroups(t *testing.T) {
	result := GetMulticastGroupSet()

	if !result[TalkGroupMcastAddr] {
		t.Errorf("expected talk group address %s in result set", TalkGroupMcastAddr)
	}
}

func TestGetMulticastGroupAddressesNoDuplicates(t *testing.T) {
	result := GetMulticastGroupAddresses()
	seen := make(map[string]bool)

	for _, addr := range result {
		if seen[addr] {
			t.Errorf("duplicate address found: %s", addr)
		}

		seen[addr] = true
	}
}

func TestGetMulticastGroupSetConsistencyWithAddresses(t *testing.T) {
	addresses := GetMulticastGroupAddresses()
	set := GetMulticastGroupSet()

	if len(addresses) != len(set) {
		t.Errorf("addresses length %d does not match set size %d", len(addresses), len(set))
	}

	for _, addr := range addresses {
		if !set[addr] {
			t.Errorf("address %s in slice but not in set", addr)
		}
	}

	for addr := range set {
		if !slices.Contains(addresses, addr) {
			t.Errorf("address %s in set but not in slice", addr)
		}
	}
}
func TestGetMulticastTalkGroupAddresses(t *testing.T) {
	result := GetMulticastTalkGroupAddresses()

	// All entries should be the shared talk group address.
	for _, addr := range result {
		if addr != TalkGroupMcastAddr {
			t.Errorf("expected %s, got %s", TalkGroupMcastAddr, addr)
		}
	}

	if len(result) != len(multicastTalkGroupPorts) {
		t.Errorf("expected %d addresses, got %d", len(multicastTalkGroupPorts), len(result))
	}
}

func TestGetMulticastTalkGroups(t *testing.T) {
	groups := GetMulticastTalkGroups()

	expectedPorts := multicastTalkGroupPorts

	if len(groups) != len(expectedPorts) {
		t.Errorf("expected %d talk groups, got %d", len(expectedPorts), len(groups))
	}

	for i, tg := range groups {
		if tg.Address != TalkGroupMcastAddr {
			t.Errorf("talk group %d: address = %s, want %s", i, tg.Address, TalkGroupMcastAddr)
		}

		if tg.Port != expectedPorts[i] {
			t.Errorf("talk group %d: port = %d, want %d", i, tg.Port, expectedPorts[i])
		}
	}
}

func TestGetMulticastTalkGroupAddressesNotNil(t *testing.T) {
	result := GetMulticastTalkGroupAddresses()

	if result == nil {
		t.Error("expected non-nil result, got nil")
	}
}

func TestGetMulticastTalkGroupAddressesLength(t *testing.T) {
	result := GetMulticastTalkGroupAddresses()
	expected := len(multicastTalkGroupPorts)

	if len(result) != expected {
		t.Errorf("expected length %d, got %d", expected, len(result))
	}
}

func TestGetMulticastTalkGroupAddressesImmutability(t *testing.T) {
	result1 := GetMulticastTalkGroupAddresses()
	result1[0] = "1.2.3.4"
	result2 := GetMulticastTalkGroupAddresses()

	if result2[0] == "1.2.3.4" {
		t.Error("modifying returned slice should not affect subsequent calls")
	}
}

func TestGetMulticastTalkGroupAddressesNoDuplicateValues(t *testing.T) {
	// All entries share the same address (TalkGroupMcastAddr); verify they
	// are indeed uniform.
	result := GetMulticastTalkGroupAddresses()

	for _, addr := range result {
		if addr != TalkGroupMcastAddr {
			t.Errorf("unexpected address %s; all should be %s", addr, TalkGroupMcastAddr)
		}
	}
}

func TestGetMulticastTalkGroupAddressesDoesNotContainNonTGAddresses(t *testing.T) {
	result := GetMulticastTalkGroupAddresses()

	for _, addr := range result {
		if addr == ATAKSAAddress || addr == ATAKChatAddress || addr == MDNSAddress {
			t.Errorf("talk group addresses should not contain non-TG address %s", addr)
		}
	}
}

func TestGetMulticastTalkGroupAddressesMatchesSource(t *testing.T) {
	result := GetMulticastTalkGroupAddresses()

	if len(result) != len(multicastTalkGroupPorts) {
		t.Errorf("expected %d entries, got %d", len(multicastTalkGroupPorts), len(result))
	}
}

// ─── TalkGroupPort tests ──────────────────────────────────────────────────────

func TestTalkGroupPort_Channel1(t *testing.T) {
	port, err := TalkGroupPort(1)
	if err != nil {
		t.Fatal(err)
	}

	if port != 38801 {
		t.Errorf("TalkGroupPort(1) = %d, want 38801", port)
	}
}

func TestTalkGroupPort_Channel2(t *testing.T) {
	port, err := TalkGroupPort(2)
	if err != nil {
		t.Fatal(err)
	}

	if port != 38803 {
		t.Errorf("TalkGroupPort(2) = %d, want 38803", port)
	}
}

func TestTalkGroupPort_Channel5(t *testing.T) {
	port, err := TalkGroupPort(5)
	if err != nil {
		t.Fatal(err)
	}

	if port != 38809 {
		t.Errorf("TalkGroupPort(5) = %d, want 38809", port)
	}
}

func TestTalkGroupPort_MaxChannel(t *testing.T) {
	port, err := TalkGroupPort(32)
	if err != nil {
		t.Fatal(err)
	}

	if port != 38863 {
		t.Errorf("TalkGroupPort(32) = %d, want 38863", port)
	}
}

func TestTalkGroupPort_Zero(t *testing.T) {
	_, err := TalkGroupPort(0)
	if err == nil {
		t.Error("expected error for channel 0")
	}
}

func TestTalkGroupPort_Negative(t *testing.T) {
	_, err := TalkGroupPort(-1)
	if err == nil {
		t.Error("expected error for negative channel")
	}
}

func TestTalkGroupPort_TooLarge(t *testing.T) {
	_, err := TalkGroupPort(33)
	if err == nil {
		t.Error("expected error for channel > 32")
	}
}

func TestTalkGroupPort_ConsistentWithPreconfigured(t *testing.T) {
	// The preconfigured multicastTalkGroupPorts should match TalkGroupPort(1..N).
	for i, want := range multicastTalkGroupPorts {
		got, err := TalkGroupPort(i + 1)
		if err != nil {
			t.Fatalf("channel %d: %v", i+1, err)
		}

		if got != want {
			t.Errorf("channel %d: TalkGroupPort = %d, preconfigured = %d", i+1, got, want)
		}
	}
}
