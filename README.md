# Overhead Aircraft

Plug in a lat/long and see aircraft that are passing overhead based on data from FlightAware Firehose.

Here's what this looks like using a point along the approach to runway 22L at KBOS: 

```
$ make run
go build
./overhead
RPA5616 (E75S) from KCMH to KBOS is 1.4nm to the northeast at 1050ft southbound at 133kts
RPA5616 (E75S) from KCMH to KBOS is 0.8nm to the northeast at 875ft southbound at 135kts
RPA5616 (E75S) from KCMH to KBOS is 0.3nm to the east at 675ft southbound at 134kts
JBU722 (A320) from KPBI to KBOS is 1.7nm to the northeast at 1175ft southbound at 131kts
JBU722 (A320) from KPBI to KBOS is 1.2nm to the northeast at 1000ft southbound at 128kts
JBU722 (A320) from KPBI to KBOS is 0.7nm to the northeast at 850ft southbound at 130kts
JBU722 (A320) from KPBI to KBOS is 0.3nm to the east at 675ft southbound at 128kts
UAL1413 (B39M) from KORD to KBOS is 1.8nm to the northeast at 1200ft southbound at 137kts
UAL1413 (B39M) from KORD to KBOS is 1.2nm to the northeast at 1000ft southbound at 135kts
UAL1413 (B39M) from KORD to KBOS is 0.6nm to the northeast at 825ft southbound at 139kts
UAL1413 (B39M) from KORD to KBOS is 0.2nm to the southeast at 625ft southbound at 138kts
AAL884 (A321) from KCLT to KBOS is 1.3nm to the northeast at 1050ft southbound at 143kts
AAL884 (A321) from KCLT to KBOS is 0.7nm to the northeast at 850ft southbound at 142kts
AAL884 (A321) from KCLT to KBOS is 0.3nm to the east at 650ft southbound at 141kts
KAP613 (C402) from KHYA to KBOS is 1.5nm to the northeast at 1100ft southbound at 137kts
KAP613 (C402) from KHYA to KBOS is 0.9nm to the northeast at 900ft southbound at 135kts
KAP613 (C402) from KHYA to KBOS is 0.4nm to the northeast at 700ft southbound at 129kts
KAP613 (C402) from KHYA to KBOS is 0.3nm to the southeast at 600ft southbound at 122kts
DAL1054 (A319) from KRDU to KBOS is 1.4nm to the northeast at 1075ft southbound at 122kts
DAL1054 (A319) from KRDU to KBOS is 0.9nm to the northeast at 900ft southbound at 122kts
DAL1054 (A319) from KRDU to KBOS is 0.4nm to the northeast at 750ft southbound at 120kts
DAL1054 (A319) from KRDU to KBOS is 0.3nm to the southeast at 575ft southbound at 120kts
ACA748 (BCS3) from CYUL to KBOS is 1.8nm to the northeast at 1200ft southbound at 145kts
ACA748 (BCS3) from CYUL to KBOS is 1.3nm to the northeast at 1025ft southbound at 133kts
ACA748 (BCS3) from CYUL to KBOS is 0.7nm to the northeast at 850ft southbound at 129kts
```

## How it works

First, any position reports that are more than 10 nautical miles away or above 15,000ft are discarded.

For each flight, the current and previous position is recorded. If the current position is within 3 nautical miles of
the configured location and is closer than the previous position was, then a message is displayed describing the
relative position and direction of the approaching aircraft.
