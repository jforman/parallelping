Parallel Ping
=============

# What is it?

A Go-binary used to ping remote hosts and return the min/max/avg/dev data for later processing. Pings are run in parallel, as serializing pings to multiple hosts does not bode well for getting consistent metrics. This Go binary integrates with several time series databases for storage and further visualization:

* [github.com/jforman/carbon-golang](http://www.github.com/jforman/carbon-golang) to send Ping metrics to a Carbon Cache.
* [github.com/influxdata/influxdb](https://github.com/influxdata/influxdb/tree/master/client/v2) to send Ping metrics to InfluxDB.

# How do I use it?

Run 'parallelping' with the appropriate flags!

### Help Output

```bash
$ go run parallelping.go --help
-destination string
  	Comma-seperated list of destinations to ping.
-interval duration
  	Seconds of wait in between each round of pings. (default 1m0s)
-ipv6
  	If set, attempt to ping via IPv6 and gather statistics.
-metricsport uint
  	Port to listen on for Prometheus metrics scrapes. (default 9110)
-oneshot
  	Execute just one ping round per host. Do not loop.
-origin string
  	Override hostname as origin with this value. (default "localhost")
-pingcount uint
  	Number of pings per cycle. (default 5)
-q	If set, only log in case of errors.
-v	If set, print out metrics as they are processed.
```

Example:

```bash
$ ./parallelping -destination www.google.com,127.0.0.1,www.test.de -interval 0m5s -pingcount 1
2021/07/01 15:44:25 Operating System determined to be: darwin.
2021/07/01 15:44:38 Destinations to ping: [www.google.com 127.0.0.1 www.test.de].
2021/07/01 15:44:38 Spawning IPv4 ping loop for 127.0.0.1.
2021/07/01 15:44:38 Spawning IPv4 ping loop for www.google.com.
2021/07/01 15:44:38 Spawning IPv4 ping loop for www.test.de.
2021/07/01 15:44:43 Spawning IPv4 ping loop for 127.0.0.1.
2021/07/01 15:44:43 Spawning IPv4 ping loop for www.google.com.
2021/07/01 15:44:48 Spawning IPv4 ping loop for 127.0.0.1.
2021/07/01 15:44:48 Spawning IPv4 ping loop for www.google.com.
```
