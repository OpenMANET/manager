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
		"225.41.1.1",
		"225.41.1.2",
		"225.41.1.3",
		"225.41.1.4",
		"225.41.1.5",
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
	expected := len(multicastGroupAddresses) + len(multicastTalkGroupAddresses)

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
		"225.41.1.1",
		"225.41.1.2",
		"225.41.1.3",
		"225.41.1.4",
		"225.41.1.5",
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
	expected := len(multicastGroupSet) + len(multicastTalkGroupAddresses)

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

	for _, addr := range multicastTalkGroupAddresses {
		if !result[addr] {
			t.Errorf("expected talk group address %s in result set", addr)
		}
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

	expectedAddresses := []string{
		"225.41.1.1",
		"225.41.1.2",
		"225.41.1.3",
		"225.41.1.4",
		"225.41.1.5",
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

func TestGetMulticastTalkGroupAddressesNotNil(t *testing.T) {
	result := GetMulticastTalkGroupAddresses()

	if result == nil {
		t.Error("expected non-nil result, got nil")
	}
}

func TestGetMulticastTalkGroupAddressesLength(t *testing.T) {
	result := GetMulticastTalkGroupAddresses()
	expected := len(multicastTalkGroupAddresses)

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

func TestGetMulticastTalkGroupAddressesNoDuplicates(t *testing.T) {
	result := GetMulticastTalkGroupAddresses()
	seen := make(map[string]bool)

	for _, addr := range result {
		if seen[addr] {
			t.Errorf("duplicate address found: %s", addr)
		}

		seen[addr] = true
	}
}

func TestGetMulticastTalkGroupAddressesDoesNotContainGroupAddresses(t *testing.T) {
	result := GetMulticastTalkGroupAddresses()

	for _, addr := range []string{ATAKSAAddress, ATAKChatAddress, MDNSAddress} {
		if slices.Contains(result, addr) {
			t.Errorf("talk group addresses should not contain group address %s", addr)
		}
	}
}

func TestGetMulticastTalkGroupAddressesMatchesSource(t *testing.T) {
	result := GetMulticastTalkGroupAddresses()

	for _, addr := range multicastTalkGroupAddresses {
		if !slices.Contains(result, addr) {
			t.Errorf("expected talk group address %s not found in result", addr)
		}
	}
}
