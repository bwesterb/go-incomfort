package main

import (
    "fmt"
    "log"

    "github.com/bwesterb/go-incomfort"
)

func main() {
    gw := incomfort.NewGateway("10.0.0.3")
    heaters, err := gw.Heaters()
    if err != nil {
        log.Fatal(err)
    }
    for _, h := range heaters {
        fmt.Printf("Heater %s\n", h.Name)
        fmt.Printf(" Setpoint       %f\n", h.Setpoint)
        fmt.Printf(" Temperature    %f\n", h.RoomTemp)
        fmt.Printf(" Display-code   %s\n", h.DisplayCode)
    }

}
