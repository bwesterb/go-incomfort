package main

import (
	"context"
	"flag"
	"log"
	"math"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwesterb/go-incomfort"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"
)

var heater incomfort.Heater
var acc *accessory.Thermostat
var updateTicker *time.Ticker

// A target set via HomeKit takes a few minutes to reach the thermostat.
// Until then we report the requested target instead of the thermostat's
// (old) setpoint, so the Home app doesn't jump back.
const pendingTimeout = 10 * time.Minute

var (
	pendingMux sync.Mutex
	pending    float64 // target set via HomeKit not yet seen; 0 if none
	pendingAt  time.Time
)

func setPending(temp float64) {
	pendingMux.Lock()
	defer pendingMux.Unlock()
	pending = temp
	pendingAt = time.Now()
}

// Returns the target temperature to report to HomeKit.
func target() float64 {
	pendingMux.Lock()
	defer pendingMux.Unlock()

	setpoint := float64(heater.Setpoint)
	if pending != 0 {
		// The thermostat rounds the setpoint down to whole degrees.
		if math.Floor(pending) == setpoint ||
			time.Since(pendingAt) > pendingTimeout {
			pending = 0
		} else {
			return pending
		}
	}
	return setpoint
}

func Update() {
	if err := heater.Update(); err != nil {
		log.Printf("Failed to update: %v", err)
		return
	}

	// Right after a reboot the gateway briefly reports (near) zero for
	// everything. Don't pass that on.
	if heater.Pressure < 0.1 && heater.RoomTemp < 1 {
		log.Printf("Skipping implausible reading (gateway rebooting?)")
		return
	}

	// We report the thermostat's setpoint rather than the gateway's
	// SetpointOverride: the latter is only set by us, and the gateway
	// forgets it when it reboots.
	acc.Thermostat.TargetTemperature.SetValue(target())
	acc.Thermostat.CurrentTemperature.SetValue(float64(heater.RoomTemp))

	if heater.IsBurning {
		acc.Thermostat.CurrentHeatingCoolingState.SetValue(
			characteristic.CurrentHeatingCoolingStateHeat)
	} else {
		acc.Thermostat.CurrentHeatingCoolingState.SetValue(
			characteristic.CurrentHeatingCoolingStateOff)
	}

	acc.Thermostat.TargetHeatingCoolingState.SetValue(
		characteristic.TargetHeatingCoolingStateAuto)
}

func main() {
	var (
		pin         string
		host        string
		addr        string
		storagePath string
		ifacesRaw   string
	)

	flag.StringVar(&pin, "pin", "00102003", "pincode")
	flag.StringVar(&addr, "addr", ":", "Local address to use")
	flag.StringVar(&host, "host", "", "hostname of incomfort LAN2RF bridge")
	flag.StringVar(&storagePath, "db", "./db", "path to local storage")
	flag.StringVar(&ifacesRaw, "ifaces", "", "comma-separated list of interfaces to advertise on")

	flag.Parse()

	var ifaces []string
	if ifacesRaw != "" {
		ifaces = strings.Split(ifacesRaw, ",")
	}

	gw := incomfort.NewGateway(host)
	if heaters, err := gw.Heaters(); err != nil {
		log.Fatalf("Failed to communicate with incomfort gateway: %v", err)
	} else {
		heater = heaters[0]
	}

	info := accessory.Info{
		Name: "IncomfortGateway",
	}
	acc = accessory.NewThermostat(info)

	fs := hap.NewFsStore(storagePath)

	s, err := hap.NewServer(fs, acc.A)
	if err != nil {
		log.Panic(err)
	}

	s.Pin = pin
	s.Addr = addr
	s.Ifaces = ifaces

	acc.Thermostat.TargetTemperature.OnValueRemoteUpdate(func(temp float64) {
		if err := heater.Set(float32(temp)); err != nil {
			log.Printf("Failed to set target to %.1f: %v", temp, err)
			setPending(0)
			return
		}
		log.Printf("Set target to %.1f", temp)
		setPending(temp)
	})

	updateTicker = time.NewTicker(time.Second * 60)
	go func() {
		for _ = range updateTicker.C {
			Update()
		}
	}()

	Update()

	ctx, cancel := context.WithCancel(context.Background())

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGTERM)
	go func() {
		<-c
		signal.Stop(c)
		cancel()
	}()

	s.ListenAndServe(ctx)
	updateTicker.Stop()
}
