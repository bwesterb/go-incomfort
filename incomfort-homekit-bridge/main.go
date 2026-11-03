package main

import (
	"context"
	"flag"
	"log"
	"math"
	"os"
	"os/signal"
	"strconv"
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

// The target set via HomeKit is authoritative: if the thermostat's setpoint
// drifts from it (for instance by the thermostat's own clock program), we set
// it again. A set takes a few minutes to reach the thermostat, so we don't
// correct more often than this.
const correctInterval = 10 * time.Minute

var (
	targetMux  sync.Mutex
	target     float64 // last target set via HomeKit; 0 if none yet
	targetPath string  // where target is kept across restarts
	lastSet    time.Time
)

func loadTarget() {
	buf, err := os.ReadFile(targetPath)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		log.Printf("Failed to read target: %v", err)
		return
	}
	t, err := strconv.ParseFloat(strings.TrimSpace(string(buf)), 64)
	if err != nil {
		log.Printf("Failed to parse target in %s: %v", targetPath, err)
		return
	}
	target = t
	log.Printf("Target is %.1f (from %s)", t, targetPath)
}

// Called when the target is changed via HomeKit.
func setTarget(temp float64) {
	targetMux.Lock()
	defer targetMux.Unlock()

	target = temp
	err := os.WriteFile(targetPath,
		[]byte(strconv.FormatFloat(temp, 'f', -1, 64)+"\n"), 0644)
	if err != nil {
		log.Printf("Failed to store target: %v", err)
	}
	log.Printf("Target set to %.1f via HomeKit", temp)
	sendLocked(temp)
}

// Sends the setpoint to the thermostat. Call with targetMux held.
func sendLocked(temp float64) {
	lastSet = time.Now()
	if err := heater.Set(float32(temp)); err != nil {
		log.Printf("Failed to set %.1f: %v", temp, err)
	}
}

// Corrects the thermostat's setpoint if it drifted from the target, and
// returns the target temperature to report to HomeKit.
func checkTarget() float64 {
	targetMux.Lock()
	defer targetMux.Unlock()

	setpoint := float64(heater.Setpoint)
	if target == 0 {
		return setpoint
	}

	// The thermostat rounds the setpoint down to whole degrees.
	if math.Floor(target) != setpoint &&
		time.Since(lastSet) > correctInterval {
		log.Printf("Setpoint is %.1f instead of %.1f: correcting",
			setpoint, target)
		sendLocked(target)
	}
	return target
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

	// Without a target we report the thermostat's setpoint rather than the
	// gateway's SetpointOverride: the latter is only set by us, and the
	// gateway forgets it when it reboots.
	acc.Thermostat.TargetTemperature.SetValue(checkTarget())
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
	flag.StringVar(&targetPath, "target", "./target", "file to keep the target temperature in")

	flag.Parse()

	loadTarget()

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

	acc.Thermostat.TargetTemperature.OnValueRemoteUpdate(setTarget)

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
