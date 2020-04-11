Parallel Ping
=============

# What is it?

A Go-binary used to ping remote hosts and return the min/max/avg/dev data for later processing. Pings are run in parallel, as serializing pings to multiple hosts does not bode well for getting consistent metrics. This Go binary integrates with several time series databases for storage and further visualization:

* [github.com/jforman/carbon-golang](http://www.github.com/jforman/carbon-golang) to send Ping metrics to a Carbon Cache.
* [github.com/influxdata/influxdb](https://github.com/influxdata/influxdb/tree/master/client/v2) to send Ping metrics to InfluxDB.

# How do I use it?

Run 'pping' with the appropriate flags!

### Help Output

```bash
$ go run pping.go --help
  -hosts string
    Comma-seperated list of hosts to ping.
  -interval duration
    Seconds of wait in between each round of pings. (default 1m0s)
  -oneshot
    Execute just one ping round per host. Do not loop.
  -pingcount uint
    Number of pings per cycle. (default 5)
  -q 
    If set, only log in case of errors.
  -receiverdatabase string
    Database for InfluxDB.
  -receiverhost string
    Hostname of metrics receiver. Optional
  -receivernoop
    If set, do not send Metrics to receiver.
  -receiverpassword string
    Password for InfluxDB database. Optional.
  -receiverport int
    Port of receiver.
  -receivertype string
    Type of receiver for statistics. Optional.
  -receiverusername string
    Username for InfluxDB database. Optional.
  -v
    If set, print out metrics as they are processed.
```

The help output should be self explanatory, both with required and optional parameters. 

An example of a parallel ping execution:

```bash
$ pping -hosts www.google.com -pingcount 2 -interval 5s -v
2016/03/27 21:16:54 Hosts to ping: [www.google.com]
2016/03/27 21:16:54 Spawning ping loop for host www.google.com.
2016/03/27 21:16:55 Ping: {origin:oyster destination:www.google.com time:1459127815 stats:{loss:0 min:20.118 avg:21.142 max:22.166 mdev:1.024}}.
2016/03/27 21:17:01 Ping: {origin:oyster destination:www.google.com time:1459127821 stats:{loss:0 min:20.951 avg:21.543 max:22.136 mdev:0.61}}.
```

The `-v` flag adds aggregate-Ping output like below if you wish:

## Integrating with carbon-golang

If you wish to send your Ping metrics to Carbon for eventual visualization in Graphite or other frontends, the required flags are `carbonhost` and `carbonport`. It is important to know that carbonport is the line receiever port and not the pickle receiver port.

Metric structure is as follows:

```bash
ping.{originating_host}.{destination_host}.{ping_metric}
```

Where ping metric is one of min, max, avg, and stddev.

## Integrating with [InfluxDB](https://influxdata.com/)

InfluxDB is a Go-based time series storage system that is meant to replace Whisper+Carbon.

If you wish to send your Ping metrics to Influx DB is it required that you specify a database on the command line via the 'receiverdatabase' flag.

Metrics are sent as 'ping' measurements with the following tags and fields:

Tags

| Name | Description |
| --- | --- |
| origin | Originating host of the pings |
| destination | Destination hostname of the pings |

Fields

| Name | Description |
| --- | --- |
| min | minimum RTT observed over Ping execution |
| avg | average RTT observed over Ping execution |
| max | maxmimum RTT observed over Ping execution |
| loss | Percentage packet loss during the ping execuion. |
| mdev | Standard deviation, or essentially the averag eof how far each ping RTT is from the mean. |


## Building and running on Docker

To automatically build a pping.go binary as /tmp/parallelping on your machine.

Using golang:1.7-onbuild image

```bash
docker run --rm -v /tmp:/tmp/buildout -v "$PWD":/usr/src/myapp -v "$GOPATH":/go -w /usr/src/myapp -e CGO_ENABLED=0 golang:1.7-onbuild go build -o /tmp/buildout/parallelping
```

To build a Docker image with the above parallelping binary:

```bash
docker build -t jforman/paralleling:latest .
```
