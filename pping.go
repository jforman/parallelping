// Ping multiple hosts in parallel

package main

import (
	"flag"
	"fmt"
	influxdbclient "github.com/influxdata/influxdb/client/v2"
	"github.com/jforman/carbon-golang"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	receiverHostFlag     string // receiverHost: Host running statistics daemon process
	receiverPortFlag     int    // receiverPort: Port running statistics daemon process
	receiverNoopFlag     bool   // If true, do not actually send the metrics to the receiver.
	receiverTypeFlag     string // receiverType: Set type of receiver for statistics
	receiverUsernameFlag string // receiverUsername: Optional username string for InfluxDB receiver.
	receiverPasswordFlag string // receiverPassword: Optional password string for InfluxDB receiver.
	receiverDatabaseFlag string // receiverDatabase: Database database string for InfluxDB receiver.

	hostsFlag          string // Flag hosts: Comma-seperated list of IP/hostnames to ping
	pingCountFlag      uint64 // Flag count: Uint8 Interger number of pings to send per cycle.
	oneshotFlag        bool
	originFlag         string // String denoting specific origin hostname used in metric submission.
	intervalFlag       time.Duration
	verboseFlag        bool
	re_ping_packetloss *regexp.Regexp
	re_ping_rtt        *regexp.Regexp
	re_ping_hostname   *regexp.Regexp
	quietFlag          bool

	pingBinary string // Path to ping binary based upon operating system)

)

type PingStats struct {
	loss float64
	min  float64
	avg  float64
	max  float64
	mdev float64
}

type Ping struct {
	origin      string
	destination string
	time        int64
	stats       PingStats
}

func isReceiverFullyDefined() bool {
	if len(receiverTypeFlag) == 0 {
		// No receiver is defined, so don't try to judge its validity.
		return true
	}
	if len(receiverHostFlag) > 0 && (receiverPortFlag > 0) {
		return true
	}
	return false
}

func checkValidReceiverType(rType string, validTypes []string) bool {
	if rType == "" {
		return true
	}
	for _, v := range validTypes {
		if rType == v {
			return true
		}
	}
	return false
}

func getDistro() string {
	contents, _ := ioutil.ReadFile("/etc/os-release")
	re_distro := regexp.MustCompile("ID=([a-zA-Z]+)")

	str_contents := string(contents)
	if verboseFlag {
		log.Printf("Contents of /etc/os-release: %v.\n", str_contents)
	}

	distro := re_distro.FindStringSubmatch(str_contents)[1]

	if verboseFlag {
		log.Printf("Distribution determined to be: %v.\n", distro)
	}
	return distro
}

func setOsParams() {
	re_ping_hostname = regexp.MustCompile(`--- (?P<hostname>\S+) ping statistics ---`)
	if !quietFlag {
		log.Printf("Operating System determined to be: %v.\n", runtime.GOOS)
	}
	switch runtime.GOOS {
	case "openbsd":
		pingBinary = "/sbin/ping"
		re_ping_packetloss = regexp.MustCompile(`(?P<loss>\d+.\d+)\% packet loss`)
		re_ping_rtt = regexp.MustCompile(`round-trip min/avg/max/std-dev = (?P<min>\d+.\d+)/(?P<avg>\d+.\d+)/(?P<max>\d+.\d+)/(?P<mdev>\d+.\d+) ms`)
	case "linux":
		pingBinary = "/bin/ping"
		re_ping_packetloss = regexp.MustCompile(`(?P<loss>\d+)\% packet loss`)
		distro := getDistro()
		switch distro {
		case "ubuntu", "debian":
			re_ping_rtt = regexp.MustCompile(`(rtt|round-trip) min/avg/max/(mdev|stddev) = (?P<min>\d+.\d+)/(?P<avg>\d+.\d+)/(?P<max>\d+.\d+)/(?P<mdev>\d+.\d+) ms`)
		case "alpine":
			re_ping_rtt = regexp.MustCompile(`round-trip min/avg/max = (?P<min>\d+.\d+)/(?P<avg>\d+.\d+)/(?P<max>\d+.\d+) ms`)
		}
	default:
		log.Fatalf("Unsupported operating system. runtime.GOOS: %v.\n", runtime.GOOS)
	}
}

func init() {
	flag.StringVar(&hostsFlag, "hosts", "", "Comma-seperated list of hosts to ping.")
	flag.Uint64Var(&pingCountFlag, "pingcount", 5, "Number of pings per cycle.")
	flag.BoolVar(&oneshotFlag, "oneshot", false, "Execute just one ping round per host. Do not loop.")
	flag.StringVar(&originFlag, "origin", "", "Override hostname as origin with this value.")
	flag.DurationVar(&intervalFlag, "interval", 60*time.Second, "Seconds of wait in between each round of pings.")
	flag.StringVar(&receiverHostFlag, "receiverhost", "", "Hostname of metrics receiver. Optional")
	flag.IntVar(&receiverPortFlag, "receiverport", 0, "Port of receiver.")
	flag.BoolVar(&receiverNoopFlag, "receivernoop", false, "If set, do not send Metrics to receiver.")
	flag.BoolVar(&verboseFlag, "v", false, "If set, print out metrics as they are processed.")
	flag.StringVar(&receiverTypeFlag, "receivertype", "", "Type of receiver for statistics. Optional.")
	flag.StringVar(&receiverUsernameFlag, "receiverusername", "", "Username for InfluxDB database. Optional.")
	flag.StringVar(&receiverPasswordFlag, "receiverpassword", "", "Password for InfluxDB database. Optional.")
	flag.StringVar(&receiverDatabaseFlag, "receiverdatabase", "", "Database for InfluxDB.")
	flag.BoolVar(&quietFlag, "q", false, "If set, only log in case of errors.")
}

// Return true if host resolves, false if not.
func doesHostExist(host string) bool {
	addresses, _ := net.LookupHost(host)
	if len(addresses) > 0 {
		return true
	}
	return false
}

func getValidHosts(hosts []string) []string {
	var trimmedHosts []string
	for _, currentHost := range hosts {
		if doesHostExist(currentHost) {
			trimmedHosts = append(trimmedHosts, currentHost)
		}
	}
	return trimmedHosts
}

func processPingOutput(pingOutput string, pingErr bool) Ping {
	var ping Ping
	var stats PingStats
	now := time.Now()
	ping.time = now.Unix()
	if len(originFlag) == 0 {
		origin, _ := os.Hostname()
		ping.origin = origin
	} else {
		ping.origin = originFlag
	}

	re_ping_hostname_matches := re_ping_hostname.FindAllStringSubmatch(pingOutput, -1)[0]
	ping.destination = re_ping_hostname_matches[1]

	re_packetloss_matches := re_ping_packetloss.FindAllStringSubmatch(pingOutput, -1)[0]

	stats.loss, _ = strconv.ParseFloat(re_packetloss_matches[1], 64)

	if pingErr == true {
		stats.min, stats.avg, stats.max, stats.mdev = 0, 0, 0, 0
	} else {
		re_rtt_matches := re_ping_rtt.FindAllStringSubmatch(pingOutput, -1)[0]
		rtt_map := make(map[string]string)
		for i, name := range re_ping_rtt.SubexpNames() {
			if i != 0 {
				rtt_map[name] = re_rtt_matches[i]
			}
		}
		stats.min, _ = strconv.ParseFloat(rtt_map["min"], 64)
		stats.avg, _ = strconv.ParseFloat(rtt_map["avg"], 64)
		stats.max, _ = strconv.ParseFloat(rtt_map["max"], 64)
		stats.mdev, _ = strconv.ParseFloat(rtt_map["mdev"], 64)
	}
	ping.stats = stats
	if !quietFlag {
		log.Printf("Ping: %+v.\n", ping)
	}
	return ping
}

func executePing(host string, numPings uint64) (string, bool) {
	pingError := false
	countFlag := fmt.Sprintf("-c%v", numPings)
	out, err := exec.Command(pingBinary, countFlag, host).Output()
	if err != nil {
		log.Printf("Error with host %s, error: %s, output: %s.\n", host, err, out)
		pingError = true
	}
	s_out := string(out[:])
	if verboseFlag {
		log.Printf("Raw Ping Output: %v.\n", s_out)
	}
	return s_out, pingError
}

func spawnPingLoop(c chan<- Ping,
	host string,
	numPings uint64,
	sleepTime time.Duration,
	oneshot bool) {
	for {
		raw_output, err := executePing(host, numPings)
		pingResult := processPingOutput(raw_output, err)
		c <- pingResult
		time.Sleep(sleepTime)

		if oneshot == true {
			break
		}
	}
}

func createCarbonMetrics(ping Ping) []carbon.Metric {
	var out []carbon.Metric
	formattedDestination := strings.Replace(ping.destination, ".", "_", -1)
	prefix := fmt.Sprintf("ping.%v.%v", ping.origin, formattedDestination)
	out = append(out, carbon.Metric{Name: fmt.Sprintf("%s.loss", prefix), Value: ping.stats.loss, Timestamp: ping.time})
	out = append(out, carbon.Metric{Name: fmt.Sprintf("%s.min", prefix), Value: ping.stats.min, Timestamp: ping.time})
	out = append(out, carbon.Metric{Name: fmt.Sprintf("%s.avg", prefix), Value: ping.stats.avg, Timestamp: ping.time})
	out = append(out, carbon.Metric{Name: fmt.Sprintf("%s.max", prefix), Value: ping.stats.max, Timestamp: ping.time})
	out = append(out, carbon.Metric{Name: fmt.Sprintf("%s.mdev", prefix), Value: ping.stats.mdev, Timestamp: ping.time})
	return out
}

func createInfluxDBMetrics(ping Ping) (influxdbclient.BatchPoints, error) {
	var err error
	bp, err := influxdbclient.NewBatchPoints(influxdbclient.BatchPointsConfig{
		Database:  receiverDatabaseFlag,
		Precision: "s",
	})
	if err != nil {
		return nil, err
	}

	tags := map[string]string{
		"origin":      ping.origin,
		"destination": ping.destination,
	}
	fields := map[string]interface{}{
		"loss": ping.stats.loss,
		"min":  ping.stats.min,
		"avg":  ping.stats.avg,
		"max":  ping.stats.max,
		"mdev": ping.stats.mdev,
	}
	pt, err := influxdbclient.NewPoint("ping", tags, fields, time.Unix(ping.time, 0))
	if err != nil {
		return nil, err
	}

	bp.AddPoint(pt)
	return bp, nil
}

func processPing(c <-chan Ping) error {
	var err error
	var carbonReceiver *carbon.Carbon
	var ic influxdbclient.Client

	switch receiverTypeFlag {
	case "carbon":
		carbonReceiver, err = carbon.NewCarbon(receiverHostFlag, receiverPortFlag, receiverNoopFlag, verboseFlag)
	case "influxdb":
		if receiverDatabaseFlag == "" {
			log.Fatalln("An InfluxDB database was not specified on the command line.")
		}
		if len(receiverUsernameFlag) > 0 {
			ic, err = influxdbclient.NewHTTPClient(
				influxdbclient.HTTPConfig{
					Addr:     fmt.Sprintf("http://%v:%v", receiverHostFlag, receiverPortFlag),
					Username: receiverUsernameFlag,
					Password: receiverPasswordFlag,
				})
		} else {
			ic, err = influxdbclient.NewHTTPClient(
				influxdbclient.HTTPConfig{
					Addr: fmt.Sprintf("http://%v:%v", receiverHostFlag, receiverPortFlag),
				})
		}
	}

	if err != nil {
		log.Printf("Error in creating connection to receiver: %v.\n", err)
	}
	for {
		pingResult := <-c
		if !isReceiverFullyDefined() {
			// A receiver is not fully defined.
			log.Printf("You receiver is not fully defined. Host: %v, Port: %v.\n", receiverHostFlag, receiverPortFlag)
			continue
		}
		switch receiverTypeFlag {
		case "carbon":
			err := carbonReceiver.SendMetrics(createCarbonMetrics(pingResult))
			if err != nil {
				log.Printf("Error sending metrics to Carbon: %v.\n", err)
				continue
			}
		case "influxdb":
			ret, err := createInfluxDBMetrics(pingResult)
			if err != nil {
				log.Fatalln(err)
			}
			err = ic.Write(ret)
			if err != nil {
				log.Printf("Error sending metrics to Influxdb: %v.\n", err)
				continue
			}
		}
		if verboseFlag {
			log.Printf("Metrics successfully written to %v receiver at %v:%v.\n", receiverTypeFlag, receiverHostFlag, receiverPortFlag)
		}

	}
	return nil
}

func main() {
	flag.Parse()
	setOsParams()

	hasValidReceiver := checkValidReceiverType(receiverTypeFlag, []string{"carbon", "influxdb"})
	if !hasValidReceiver {
		log.Fatalf("You specified an unsupported receiver type %v.\n", receiverTypeFlag)
	}

	hosts := strings.Split(hostsFlag, ",")
	validHosts := getValidHosts(hosts)
	if !quietFlag {
		log.Printf("Hosts to ping: %v\n", validHosts)
	}
	var c chan Ping = make(chan Ping)
	for _, currentHost := range validHosts {
		if !quietFlag {
			log.Printf("Spawning ping loop for host %v.\n", currentHost)
		}
		go spawnPingLoop(c, currentHost, pingCountFlag, intervalFlag, oneshotFlag)
	}
	err := processPing(c)
	if err != nil {
		log.Printf("Error in call to processPing: %v.\n", err)
	}
}
