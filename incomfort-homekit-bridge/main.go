package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
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

func Update() {
	heater.Update()
	acc.Thermostat.TargetTemperature.SetValue(float64(heater.SetpointOverride))
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
		heater.Set(float32(temp))
	})

	updateTicker = time.NewTicker(time.Second * 60)
	go func() {
		for _ = range updateTicker.C {
			Update()
		}
	}()

	Update()

	ctx, cancel := context.WithCancel(context.Background())

	c := make(chan os.Signal)
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
