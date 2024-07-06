# Overhead Aircraft

Plug in a lat/long and see aircraft that are passing overhead based on data from FlightAware Firehose.

Here's what this looks like using a point along the approach to runway 22L at KBOS: 

```
$ make run
go build
./overhead
[10:02:13] UAL641 (B39M) from KIAD to KBOS is 1.4nm to the northeast at 1050ft southbound at 141kts
           https://www.flightaware.com/live/flight/id/UAL641-1720083075-fa-2029p
[10:02:29] UAL641 (B39M) from KIAD to KBOS is 0.8nm to the northeast at 875ft southbound at 140kts
           https://www.flightaware.com/live/flight/id/UAL641-1720083075-fa-2029p
[10:02:45] UAL641 (B39M) from KIAD to KBOS is 0.3nm to the east at 650ft southbound at 141kts
           https://www.flightaware.com/live/flight/id/UAL641-1720083075-fa-2029p
[10:03:58] RPA4376 (E75S) from KJFK to KBOS is 1.9nm to the northeast at 1200ft southbound at 126kts
           https://www.flightaware.com/live/flight/id/RPA4376-1720097566-schedule-289p
[10:04:14] RPA4376 (E75S) from KJFK to KBOS is 1.3nm to the northeast at 1025ft southbound at 124kts
           https://www.flightaware.com/live/flight/id/RPA4376-1720097566-schedule-289p
[10:04:30] RPA4376 (E75S) from KJFK to KBOS is 0.8nm to the northeast at 875ft southbound at 122kts
           https://www.flightaware.com/live/flight/id/RPA4376-1720097566-schedule-289p
[10:04:46] RPA4376 (E75S) from KJFK to KBOS is 0.3nm to the east at 700ft southbound at 124kts
           https://www.flightaware.com/live/flight/id/RPA4376-1720097566-schedule-289p
```

## How it works

First, any position reports that are more than 10 nautical miles away or above 15,000ft are discarded.

For each flight, the current and previous position is recorded. If the current position is within 3 nautical miles of
the configured location and is closer than the previous position was, then a message is displayed describing the
relative position and direction of the approaching aircraft.

## How to use it

Edit `overhead.toml` by filling in your Firehose credentials and the location you're interested in.

Then run `go build` and then `./overhead`.