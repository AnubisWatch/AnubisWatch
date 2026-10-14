package raft

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func regressionReplayCacheMessage(t *testing.T, now time.Time, nonce int) *GossipMessage {
	t.Helper()
	msg := &GossipMessage{Type: "gossip", NodeID: "fixture-peer", Timestamp: now.Unix(), Nonce: fmt.Sprint(nonce)}
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte("audit-fixture-secret"))
	mac.Write(body)
	msg.HMAC = hex.EncodeToString(mac.Sum(nil))
	return msg
}

func regressionReplayCacheAdmit(t *testing.T, d *Discovery, msg *GossipMessage, now time.Time) error {
	t.Helper()
	if err := verifyGossip(msg, []byte("audit-fixture-secret"), d.messageFreshness, now); err != nil {
		t.Fatalf("fixture must authenticate: %v", err)
	}
	return d.checkReplay(msg.HMAC, now)
}

func TestDiscoveryReplayCacheRetainsFreshSignatures(t *testing.T) {
	now := time.Unix(1700000000, 0)
	d := &Discovery{messageFreshness: 5 * time.Minute}
	original := regressionReplayCacheMessage(t, now, 0)
	if err := regressionReplayCacheAdmit(t, d, original, now); err != nil {
		t.Fatal(err)
	}
	if err := regressionReplayCacheAdmit(t, d, original, now); !errors.Is(err, ErrMessageStale) {
		t.Fatalf("control duplicate: %v", err)
	}
	for i := 1; i < seenHMACsMax; i++ {
		if err := regressionReplayCacheAdmit(t, d, regressionReplayCacheMessage(t, now, i), now); err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}
	if got := len(d.seenHMACs); got != seenHMACsMax {
		t.Fatalf("exact capacity: got %d", got)
	}
	for i := 0; i < 3; i++ {
		if err := regressionReplayCacheAdmit(t, d, regressionReplayCacheMessage(t, now, seenHMACsMax+i), now); err == nil {
			t.Fatal("overflow must reject a new signature")
		}
		if err := regressionReplayCacheAdmit(t, d, original, now); !errors.Is(err, ErrMessageStale) {
			t.Fatalf("original duplicate after overflow: %v", err)
		}
	}
	// Every accepted signature must survive rejected overflow attempts.
	for i := 0; i < seenHMACsMax; i++ {
		if err := regressionReplayCacheAdmit(t, d, regressionReplayCacheMessage(t, now, i), now); !errors.Is(err, ErrMessageStale) {
			t.Fatalf("lost signature %d: %v", i, err)
		}
	}
	boundary := now.Add(d.messageFreshness)
	if err := regressionReplayCacheAdmit(t, d, original, boundary); !errors.Is(err, ErrMessageStale) {
		t.Fatalf("duplicate at inclusive expiry boundary: %v", err)
	}
	if err := regressionReplayCacheAdmit(t, d, regressionReplayCacheMessage(t, boundary, seenHMACsMax+3), boundary); err == nil {
		t.Fatal("inclusive boundary must still retain fresh entries")
	}
	after := boundary.Add(time.Nanosecond)
	if err := verifyGossip(original, []byte("audit-fixture-secret"), d.messageFreshness, after); !errors.Is(err, ErrMessageStale) {
		t.Fatalf("expired original must fail timestamp verification: %v", err)
	}
	fresh := regressionReplayCacheMessage(t, after, seenHMACsMax+4)
	if err := regressionReplayCacheAdmit(t, d, fresh, after); err != nil {
		t.Fatalf("new message after expiry: %v", err)
	}
	if got := len(d.seenHMACs); got != 1 {
		t.Fatalf("pruned capacity: got %d, want 1", got)
	}
	if err := d.checkReplay("", after); err != nil {
		t.Fatalf("unsigned control: %v", err)
	}
}
