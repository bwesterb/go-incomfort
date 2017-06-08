package main

import (
	"flag"
	"github.com/brutella/hc"
	"github.com/brutella/hc/accessory"
	"github.com/brutella/hc/characteristic"
	"github.com/bwesterb/go-incomfort"
	"log"
	"time"
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
	var pin string
	var host string
	var port int
	var storagePath string

	flag.StringVar(&pin, "pin", "00102003", "pincode")
	flag.IntVar(&port, "port", 0, "Local port to use")
	flag.StringVar(&host, "host", "", "hostname of incomfort LAN2RF bridge")
	flag.StringVar(&storagePath, "db", "./db", "path to local storage")

	flag.Parse()

	gw := incomfort.NewGateway(host)
	if heaters, err := gw.Heaters(); err != nil {
		log.Fatalf("Failed to communicate with incomfort gateway: %v", err)
	} else {
		heater = heaters[0]
	}

	info := accessory.Info{
		Name: "Incomfort Gateway",
	}
	acc = accessory.NewThermostat(info, float64(heater.RoomTemp), 5.0, 30.0, 0.5)

	var portString = ""
	if port != 0 {
		portString = string(port)
	}
	config := hc.Config{
		Pin:         pin,
		Port:        portString,
		StoragePath: storagePath,
	}
	t, err := hc.NewIPTransport(config, acc.Accessory)
	if err != nil {
		log.Panic(err)
	}

	acc.Thermostat.TargetTemperature.OnValueRemoteUpdate(func(temp float64) {
		heater.Set(float32(temp))
	})

	updateTicker = time.NewTicker(time.Second * 60) // TODO
	go func() {
		for _ = range updateTicker.C {
			Update()
		}
	}()

	Update()

	hc.OnTermination(func() {
		t.Stop()
		updateTicker.Stop()
	})

	t.Start()
}
