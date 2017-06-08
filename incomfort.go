// Control thermostat via the incomfort LAN2RF gateway

package incomfort

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

var displayCodes = map[int]string{
	85:  "sensortest",
	170: "service",
	204: "tapwater",
	51:  "tapwater int.",
	240: "boiler int.",
	15:  "boiler ext.",
	153: "postrun boiler",
	102: "central heating",
	0:   "opentherm",
	255: "buffer",
	24:  "frost",
	231: "postrun ch",
	126: "standby",
	37:  "central heating rf",
}

func lsbMsb(lsb, msb int) float32 {
	return float32(lsb+msb*256) / 100
}

// Response of the gateway to a heaterlist request
type heaterListData struct {
	HeaterList []*string `json:"heaterlist"`
}

// Response of the gateway to a heater request
type heaterData struct {
	NodeNr          int `json:"nodenr"`
	ChTempLsb       int `json:"ch_temp_lsb"`
	ChTempMsb       int `json:"ch_temp_msb"`
	TapTempLsb      int `json:"tap_temp_lsb"`
	TapTempMsb      int `json:"tap_temp_msb"`
	ChPressureLsb   int `json:"ch_pressure_lsb"`
	ChPressureMsb   int `json:"ch_pressure_msb"`
	RoomTemp1Lsb    int `json:"room_temp_1_lsb"`
	RoomTemp1Msb    int `json:"room_temp_1_msb"`
	RoomTempSet1Lsb int `json:"room_temp_set_1_lsb"`
	RoomTempSet1Msb int `json:"room_temp_set_1_msb"`
	RoomTemp2Lsb    int `json:"room_temp_2_lsb"`
	RoomTemp2Msb    int `json:"room_temp_2_msb"`
	RoomTempSet2Lsb int `json:"room_temp_set_2_lsb"`
	RoomTempSet2Msb int `json:"room_temp_set_2_msb"`
	DisplCode       int `json:"displ_code"`
	IO              int `json:"IO"`
	SerialYear      int `json:"serial_year"`
	SerialMonth     int `json:"serial_month"`
	SerialLine      int `json:"serial_line"`
	SerialSn1       int `json:"serial_sn1"`
	SerialSn2       int `json:"serial_sn2"`
	SerialSn3       int `json:"serial_sn3"`
	RoomSetOvr1Msb  int `json:"room_set_ovr_1_msb"`
	RoomSetOvr1Lsb  int `json:"room_set_ovr_1_lsb"`
	RoomSetOvr2Msb  int `json:"room_set_ovr_2_msb"`
	RoomSetOvr2Lsb  int `json:"room_set_ovr_2_lsb"`
	RfMessageRssi   int `json:"rf_message_rssi"`
	RfStatusCntr    int `json:"rfstatus_cntr"`
}

// Represent the incomfort LAN2RF gateway
type Gateway struct {
	Host string
}

// Represent a heater
type Heater struct {
	gw   *Gateway
	Id   int
	Name string

	Pressure         float32
	HeaterTemp       float32
	TapTemp          float32
	RoomTemp         float32
	Setpoint         float32
	SetpointOverride float32
	DisplayCode      string
	IsBurning        bool
	IsLockout        bool
	IsPumping        bool
	IsTapping        bool
}

func NewGateway(host string) *Gateway {
	return &Gateway{host}
}

func (g *Gateway) doGet(path string, data interface{}) error {
	resp, err := http.Get("http://" + g.Host + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if data == nil {
		return nil
	}

	return json.Unmarshal(body, data)
}

// Returns the list of heaters
func (g *Gateway) Heaters() (heaters []Heater, err error) {
	heaters = []Heater{}
	var data heaterListData

	err2 := g.doGet("/heaterlist.json", &data)
	if err2 != nil {
		return nil, err2
	}

	for i, h := range data.HeaterList {
		if h == nil {
			continue
		}
		heater := Heater{
			gw:   g,
			Id:   i,
			Name: *h,
		}
		heater.Update()
		heaters = append(heaters, heater)
	}
	return
}

// Updates the data
func (h *Heater) Update() error {
	var data heaterData

	err := h.gw.doGet(fmt.Sprintf("/data.json?heater=%d", h.Id), &data)
	if err != nil {
		return err
	}

	h.Pressure = lsbMsb(data.ChPressureLsb, data.ChPressureMsb)
	h.HeaterTemp = lsbMsb(data.ChTempLsb, data.ChTempMsb)
	h.TapTemp = lsbMsb(data.TapTempLsb, data.TapTempMsb)
	h.RoomTemp = lsbMsb(data.RoomTemp1Lsb, data.RoomTemp1Msb)
	h.Setpoint = lsbMsb(data.RoomTempSet1Lsb, data.RoomTempSet1Msb)
	h.SetpointOverride = lsbMsb(data.RoomSetOvr1Lsb, data.RoomSetOvr1Msb)

	if dc, ok := displayCodes[data.DisplCode]; ok {
		h.DisplayCode = dc
	} else {
		h.DisplayCode = fmt.Sprintf("unknown: %d", data.DisplCode)
	}

	h.IsBurning = data.IO&8 != 0
	h.IsLockout = data.IO&1 != 0
	h.IsPumping = data.IO&2 != 0
	h.IsTapping = data.IO&4 != 0

	return nil
}

// Set temperature
func (h *Heater) Set(temp float32) error {
	if temp < 5 {
		temp = 5
	}
	if temp > 30 {
		temp = 30
	}
	return h.gw.doGet(fmt.Sprintf(
		"/data.json?heater=%d&thermostat=0&setpoint=%d",
		h.Id, int((temp-5)*10)), nil)
}
