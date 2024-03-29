package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tarm/serial"
	"github.com/tonygilkerson/mbx-iot/pkg/iot"
)

// Main
func main() {

	// Log to the console with date, time and filename prepended
	log.SetFlags(log.LstdFlags | log.Llongfile)

	//
	// Get environment Variables
	//
	serialPort, exists := os.LookupEnv("SERIAL_PORT")
	if !exists {
		log.Fatalln("SERIAL_PORT environment variable not set")
	}

	log.Printf("Using SERIAL_PORT=%s", serialPort)

	//
	// open serial device
	// Device is something like "/dev/ttyUSB0"
	//
	cfg := &serial.Config{Name: serialPort, Baud: 115200}
	port, err := serial.OpenPort(cfg)
	if err != nil {
		log.Panicf("Error could not open serial port %q. %v\n", serialPort, err)
	}

	//
	// Start serialServer
	//
	go serialServer(port)

	//
	// Server up API endpoints
	//
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/pub", func(w http.ResponseWriter, r *http.Request) { pubMsg(w, r, port) })
	http.ListenAndServe(":8080", nil)

}

// /////////////////////////////////////////////////////////////////////////////
//
//	Functions
//
// /////////////////////////////////////////////////////////////////////////////
func pubMsg(w http.ResponseWriter, r *http.Request, port *serial.Port) {

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Println(err)
		fmt.Fprintf(w, "error")
	} else {
		fmt.Fprintf(w, "ok")
	}

	//
	//  Write to serial port
	//
	log.Printf("Write to serial port [%v]", string(body))
	_, err = port.Write(body)
	if err != nil {
		log.Printf("Error writing to serial port [%v]\n", err)
	}
}

func serialServer(port *serial.Port) {

	//
	// MailboxDoorOpened
	//
	var mbxMailboxDoorOpenedCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mbx_mailbox_door_opened_count",
			Help: "No of times the mailbox door has been opened",
		},
	)
	prometheus.MustRegister(mbxMailboxDoorOpenedCount)

	//
	// MuleAlarm
	//
	var mbxMuleAlarmCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mbx_mule_alarm_count",
			Help: "No of times the mule alarm has gone off",
		},
	)
	prometheus.MustRegister(mbxMuleAlarmCount)

	//
	// Mailbox Temperature
	//
	var mbxTemperatureFahrenheit = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "mbx_temperature_fahrenheit",
			Help: "The temperature reading in fahrenheit from the device on the mailbox",
		},
	)
	prometheus.MustRegister(mbxTemperatureFahrenheit)

	//
	// Charge Status
	//
	var mbxChargerChargeStatus = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "mbx_charger_charge_status",
			Help: "The charger's charge status, 0=off, 1=on",
		},
	)
	prometheus.MustRegister(mbxChargerChargeStatus)

	//
	// Charge Power Source  ChargerPowerSourceGood
	//
	var mbxChargerPowerStatus = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "mbx_charger_power_status",
			Help: "The charger's power source status, 0=bad, 1=good",
		},
	)
	prometheus.MustRegister(mbxChargerPowerStatus)

	//
	// Define a counter to keep track of the number of mbx heartbeats
	//
	var mbxRoadMainLoopHeartbeatCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mbx_road_main_loop_heartbeat_count",
			Help: "Heartbeat counter for the main loop for the device down on the road",
		},
	)
	prometheus.MustRegister(mbxRoadMainLoopHeartbeatCount)

	//
	// Soil Temperature
	//
	var soilTemperatureFahrenheit = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "soil_temperature_fahrenheit",
			Help: "The temperature reading in fahrenheit from the device in the goat field",
		},
	)
	prometheus.MustRegister(soilTemperatureFahrenheit)

	//
	// Soil Moisture
	//
	var soilMoisture = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "soil_moisture",
			Help: "The moisture reading from the device in the goat field, can range from about 300 to 1,000",
		},
	)
	prometheus.MustRegister(soilMoisture)

	//
	// Monitor the serial port forever
	//
	buf := make([]byte, 128)
	log.Println("Start read loop for serial port")

	var partialMsg string

	for {

		var err error
		var n int

		log.Println("read serial port")

		n, err = port.Read(buf)
		if err != nil {
			log.Panicf("Error trying to read serial port %v\n", err)
		}

		//
		// messages should looks like "msg1|msg2|msg3|" and end in a |
		//
		log.Printf("received %v bytes, buff: %v",n,string(buf[:n]))
		messages := partialMsg + string(buf[:n])

		log.Printf("messages: %v",messages)

		// prepend the partial message from last time to the message we got this time
		// if we don't find a | then we still have a partial message
		// Add to the partial message and keep reading
		if !strings.HasSuffix(messages, "|") {
			partialMsg = messages
			log.Printf("no terminator found, this is a partial message, set partialMsg and continue: %v",partialMsg)
			continue
		} else {
			log.Printf("found terminator, clear partialMsg\n")
			partialMsg = ""
		}

		log.Printf("messages that need split: %v",messages)
		//
		// Split
		msgs := strings.Split(messages, "|")

		log.Printf("msgs: %v",msgs)
		for _, msg := range msgs {

			switch {

			case strings.Contains(msg, string(iot.MbxTemperature)):
				parts := strings.Split(msg, ":")
				f, err := strconv.ParseFloat(parts[1], 64)
				if err != nil {
					log.Printf("Error converting temperature reading to a float, original input message: %v, error: %v", msg, err)
				} else {
					mbxTemperatureFahrenheit.Set(f)
					log.Printf("ACTION: set MailboxTemperature to: %v", f)
				}

			case msg == iot.MbxMuleAlarm:
				mbxMuleAlarmCount.Inc()
				log.Println("ACTION: increment mbxMuleAlarmCount")

			case msg == iot.MbxDoorOpened:
				mbxMailboxDoorOpenedCount.Inc()
				log.Println("ACTION: increment mbxMailboxDoorOpenedCount")

			case msg == iot.MbxChargerChargeStatusOn:
				mbxChargerChargeStatus.Set(1)
				log.Println("ACTION: set mbxChargerChargeStatus to ON")

			case msg == iot.MbxChargerChargeStatusOff:
				mbxChargerChargeStatus.Set(0)
				log.Println("ACTION: set mbxChargerChargeStatus to OFF")

			case msg == iot.MbxChargerPowerSourceGood:
				mbxChargerPowerStatus.Set(1)
				log.Println("ACTION: set mbxChargerPowerStatus to GOOD")

			case msg == iot.MbxChargerPowerSourceBad:
				mbxChargerPowerStatus.Set(0)
				log.Println("ACTION: set mbxChargerPowerStatus to BAD")

			case msg == iot.MbxRoadMainLoopHeartbeat:
				mbxRoadMainLoopHeartbeatCount.Inc()
				log.Println("ACTION: increment mbxRoadMainLoopHeartbeatCount")

			case strings.Contains(msg, string(iot.SoilTemperature)):
				parts := strings.Split(msg, ":")
				f, err := strconv.ParseFloat(parts[1], 64)
				if err != nil {
					log.Printf("Error converting soil temperature reading to a float, original input message: %v, error: %v", msg, err)
				} else {
					soilTemperatureFahrenheit.Set(f)
					log.Printf("ACTION: set soilTemperatureFahrenheit to: %v", f)
				}

			case strings.Contains(msg, string(iot.SoilMoisture)):
				parts := strings.Split(msg, ":")
				f, err := strconv.ParseFloat(parts[1], 64)
				if err != nil {
					log.Printf("Error converting soil moisture reading to a float, original input message: %v, error: %v", msg, err)
				} else {
					soilMoisture.Set(f)
					log.Printf("ACTION: set soilMoisture to: %v", f)
				}

			case msg == "":
				// eat whitespace
				
			default:
				log.Printf("ACTION: No-op: %s\n", msg)
			}
		}
	}
}
