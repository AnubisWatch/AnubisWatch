package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"math"
	"testing"
)

var alertEncodingRegressionError = errors.New("fixture marshal failure")

type alertEncodingRegressionFailure struct{}

func (alertEncodingRegressionFailure) MarshalJSON() ([]byte, error) {
	return nil, alertEncodingRegressionError
}
func alertEncodingRegressionRecord(db *CobaltDB, kind, id string, value any) (func() error, string) {
	if kind == "channel" {
		return func() error {
			return db.SaveAlertChannel(&core.AlertChannel{ID: id, Config: map[string]interface{}{"fixture": value}})
		}, "default/alerts/channels/" + id
	}
	return func() error {
		return db.SaveAlertRule(&core.AlertRule{ID: id, Conditions: []core.AlertCondition{{Type: "threshold", Value: value}}})
	}, "default/alerts/rules/" + id
}
func TestAlertEncodingFailurePreservesStoredRecords(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	for _, kind := range []string{"channel", "rule"} {
		save, key := alertEncodingRegressionRecord(db, kind, "fixture-"+kind, 1)
		if e := save(); e != nil {
			t.Fatal(e)
		}
		before, e := db.Get(key)
		if e != nil {
			t.Fatal(e)
		}
		for _, value := range []any{math.NaN(), make(chan int), alertEncodingRegressionFailure{}} {
			bad, _ := alertEncodingRegressionRecord(db, kind, "fixture-"+kind, value)
			e := bad()
			if e == nil {
				t.Fatal("swallowed error")
			}
			if _, ok := value.(alertEncodingRegressionFailure); ok && !errors.Is(e, alertEncodingRegressionError) {
				t.Fatal("lost wrapped cause")
			}
			after, e := db.Get(key)
			if e != nil || !bytes.Equal(before, after) {
				t.Fatal("invalid update changed record")
			}
		}
		bad, newKey := alertEncodingRegressionRecord(db, kind, "new-"+kind, math.Inf(1))
		if e := bad(); e == nil {
			t.Fatal("invalid create")
		}
		if _, e := db.Get(newKey); e == nil {
			t.Fatal("invalid create persisted")
		}
		db.mu.RLock()
		_, channelIndex := db.channelIndex["new-"+kind]
		_, ruleIndex := db.ruleIndex["new-"+kind]
		db.mu.RUnlock()
		if channelIndex || ruleIndex {
			t.Fatal("invalid create indexed")
		}
		valid, _ := alertEncodingRegressionRecord(db, kind, "fixture-"+kind, nil)
		if e := valid(); e != nil {
			t.Fatal("valid null")
		}
		raw, e := db.Get(key)
		if e != nil || !json.Valid(raw) {
			t.Fatal("null roundtrip")
		}
	}
}
