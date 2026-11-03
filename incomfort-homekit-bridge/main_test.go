package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwesterb/go-incomfort"
)

// A fake LAN2RF gateway with a thermostat at the given setpoint.
type fakeGateway struct {
	mux      sync.Mutex
	setpoint int   // in centidegrees
	sets     []int // setpoint parameters received
}

func (f *fakeGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mux.Lock()
	defer f.mux.Unlock()
	switch r.URL.Path {
	case "/heaterlist.json":
		fmt.Fprint(w, `{"heaterlist": ["1234", null, null, null, null, null, null, null]}`)
	case "/data.json":
		if sp := r.URL.Query().Get("setpoint"); sp != "" {
			v, _ := strconv.Atoi(sp)
			f.sets = append(f.sets, v)
		}
		fmt.Fprintf(w, `{"nodenr": 1, "ch_pressure_lsb": 134, "ch_pressure_msb": 0,
			"room_temp_1_lsb": %d, "room_temp_1_msb": %d,
			"room_temp_set_1_lsb": %d, "room_temp_set_1_msb": %d}`,
			2240%256, 2240/256, f.setpoint%256, f.setpoint/256)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeGateway) nSets() int {
	f.mux.Lock()
	defer f.mux.Unlock()
	return len(f.sets)
}

func TestTarget(t *testing.T) {
	fake := &fakeGateway{setpoint: 2200}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	heaters, err := incomfort.NewGateway(
		strings.TrimPrefix(srv.URL, "http://")).Heaters()
	if err != nil {
		t.Fatal(err)
	}
	heater = heaters[0]
	targetPath = filepath.Join(t.TempDir(), "target")
	target, lastSet = 0, time.Time{}

	poll := func() float64 {
		if err := heater.Update(); err != nil {
			t.Fatal(err)
		}
		return checkTarget()
	}

	// No target yet: report the setpoint, don't touch the thermostat.
	if got := poll(); got != 22 || fake.nSets() != 0 {
		t.Fatalf("no target: reported %v, %d sets", got, fake.nSets())
	}

	// HomeKit sets 20.5: sent to the gateway as (20.5-5)*10.
	setTarget(20.5)
	if fake.nSets() != 1 || fake.sets[0] != 155 {
		t.Fatalf("setTarget: sets %v", fake.sets)
	}

	// Not arrived yet: report the target, don't resend within the interval.
	if got := poll(); got != 20.5 || fake.nSets() != 1 {
		t.Fatalf("pending: reported %v, %d sets", got, fake.nSets())
	}

	// Arrived, rounded down to 20: no correction, even after the interval.
	fake.setpoint = 2000
	lastSet = time.Now().Add(-time.Hour)
	if got := poll(); got != 20.5 || fake.nSets() != 1 {
		t.Fatalf("arrived: reported %v, %d sets", got, fake.nSets())
	}

	// The clock program changes it to 15: corrected.
	fake.setpoint = 1500
	if got := poll(); got != 20.5 || fake.nSets() != 2 || fake.sets[1] != 155 {
		t.Fatalf("drift: reported %v, sets %v", got, fake.sets)
	}

	// Still 15 right after the correction: wait for the interval.
	if poll(); fake.nSets() != 2 {
		t.Fatalf("drift, within interval: sets %v", fake.sets)
	}

	// A restart picks the target up again.
	target = 0
	loadTarget()
	if target != 20.5 {
		t.Fatalf("loadTarget: %v", target)
	}
}
