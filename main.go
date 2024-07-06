package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/benburwell/firehose"
	"github.com/kelseyhightower/envconfig"
	"github.com/skypies/geo"
)

const (
	InterestingRadiusNM            float64 = 10
	NotificationThresholdNM        float64 = 3
	InterestingAltitudeCeilingFeet float64 = 15000
)

func main() {
	var app App

	if err := envconfig.Process("", &app); err != nil {
		log.Fatal(err.Error())
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type App struct {
	Username  string  `envconfig:"FIREHOSE_USERNAME"`
	Password  string  `envconfig:"FIREHOSE_PASSWORD"`
	Latitude  float64 `envconfig:"LATITUDE"`
	Longitude float64 `envconfig:"LONGITUDE"`

	flights map[string]*position
}

func (a *App) Run(ctx context.Context) error {
	box := a.flightObservationBox()

	stream, err := firehose.Connect()
	if err != nil {
		return fmt.Errorf("could not establish Firehose connection: %w", err)
	}
	defer stream.Close()

	cmd := firehose.InitCommand{
		Live:     true,
		Username: a.Username,
		Password: a.Password,
		Events:   []firehose.Event{firehose.PositionEvent},
		LatLong:  []firehose.Rectangle{box},
	}

	if err := stream.Init(cmd.String()); err != nil {
		return fmt.Errorf("could not initialize firehose: %w", err)
	}

	for {
		msg, err := stream.NextMessage(ctx)
		if errors.Is(err, context.Canceled) {
			return nil
		} else if err != nil {
			return err
		}
		switch m := msg.Payload.(type) {
		case firehose.PositionMessage:
			a.handlePosition(&m)
		case firehose.ErrorMessage:
			return fmt.Errorf("firehose error: %s", m.ErrorMessage)
		}
	}
}

func (a *App) flightObservationBox() firehose.Rectangle {
	center := a.myLocation()
	minLat := center.MoveNM(180, InterestingRadiusNM)
	maxLat := center.MoveNM(0, InterestingRadiusNM)
	minLon := center.MoveNM(270, InterestingRadiusNM)
	maxLon := center.MoveNM(90, InterestingRadiusNM)
	return firehose.Rectangle{
		LowLat: minLat.Lat,
		LowLon: minLon.Long,
		HiLat:  maxLat.Lat,
		HiLon:  maxLon.Long,
	}
}

func (a *App) isInteresting(pos *position) bool {
	dist := pos.point.DistNM(a.myLocation())
	if dist > InterestingRadiusNM {
		return false
	}
	if pos.altitude != nil && *pos.altitude > InterestingAltitudeCeilingFeet {
		return false
	}
	return true
}

type position struct {
	flightID     string
	point        geo.Latlong
	altitude     *float64
	ident        string
	reg          string
	origin       string
	destination  string
	aircraftType string
	speed        *float64
	heading      *float64
	timestamp    time.Time
}

func newPosition(msg *firehose.PositionMessage) (*position, error) {
	var pos position
	pos.flightID = msg.ID
	lat, err := strconv.ParseFloat(msg.Lat, 64)
	if err != nil {
		return nil, fmt.Errorf("lat: %w", err)
	}
	lon, err := strconv.ParseFloat(msg.Lon, 64)
	if err != nil {
		return nil, fmt.Errorf("lon: %w", err)
	}
	pos.point = geo.Latlong{
		Lat:  lat,
		Long: lon,
	}
	if msg.Alt != "" {
		alt, err := strconv.ParseFloat(msg.Alt, 64)
		if err != nil {
			return nil, fmt.Errorf("alt: %w", err)
		}
		pos.altitude = &alt
	}
	pos.ident = msg.Ident
	pos.reg = msg.Reg
	pos.origin = msg.Orig
	pos.destination = msg.Dest
	pos.aircraftType = msg.AircraftType
	if msg.GS != "" {
		gs, err := strconv.ParseFloat(msg.GS, 64)
		if err != nil {
			return nil, fmt.Errorf("gs: %w", err)
		}
		pos.speed = &gs
	}
	var heading string
	if msg.Heading != "" {
		heading = msg.Heading
	}
	if msg.HeadingTrue != "" {
		heading = msg.HeadingTrue
	}
	if heading != "" {
		hdg, err := strconv.ParseFloat(heading, 64)
		if err != nil {
			return nil, fmt.Errorf("heading: %w", err)
		}
		pos.heading = &hdg
	}
	clock, err := strconv.ParseInt(msg.Clock, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("clock: %w", err)
	}
	pos.timestamp = time.Unix(clock, 0)
	return &pos, nil
}

func (a *App) myLocation() geo.Latlong {
	return geo.Latlong{
		Lat:  a.Latitude,
		Long: a.Longitude,
	}
}

func (a *App) handlePosition(msg *firehose.PositionMessage) {
	curr, err := newPosition(msg)
	if err != nil {
		log.Printf("could not translate position message: %v", err)
		return
	}
	if !a.isInteresting(curr) {
		return
	}

	if a.flights == nil {
		a.flights = make(map[string]*position)
	}
	if prev, ok := a.flights[curr.flightID]; ok {
		me := a.myLocation()
		distToPrev := prev.point.DistNM(me)
		distToCurr := curr.point.DistNM(me)
		if distToCurr < distToPrev && distToCurr < NotificationThresholdNM {
			a.displayFlight(curr)
		}
	}
	a.flights[curr.flightID] = curr
}

func (a *App) displayFlight(curr *position) {
	me := a.myLocation()
	dist := curr.point.DistNM(me)
	bearing := me.BearingTowards(curr.point)

	var alert strings.Builder

	alert.WriteString(fmt.Sprintf("[%s] ", curr.timestamp.Format("15:04:05")))

	alert.WriteString(curr.ident)
	if curr.aircraftType != "" {
		alert.WriteString(" (" + curr.aircraftType + ")")
	}
	alert.WriteString(" from " + curr.origin)
	if curr.destination != "" {
		alert.WriteString(" to " + curr.destination)
	}
	alert.WriteString(fmt.Sprintf(" is %.1fnm to the %s", dist, cardinalDirection(bearing)))
	if curr.altitude != nil {
		alert.WriteString(fmt.Sprintf(" at %.0fft", *curr.altitude))
	}
	dir := "travelling"
	if curr.heading != nil {
		dir = cardinalDirection(*curr.heading) + "bound"
	}
	if curr.speed != nil {
		alert.WriteString(fmt.Sprintf(" %s at %.0fkts", dir, *curr.speed))
	}

	alert.WriteString(fmt.Sprintf("\n\thttps://www.flightaware.com/live/flight/id/%s", curr.flightID))

	fmt.Println(alert.String())
}

func cardinalDirection(bearing float64) string {
	if bearing > 337.5 || bearing <= 22.5 {
		return "north"
	}
	if bearing > 22.5 && bearing <= 67.5 {
		return "northeast"
	}
	if bearing > 67.5 && bearing <= 112.5 {
		return "east"
	}
	if bearing > 112.5 && bearing <= 157.5 {
		return "southeast"
	}
	if bearing > 157.5 && bearing <= 202.5 {
		return "south"
	}
	if bearing > 202.5 && bearing <= 247.5 {
		return "southwest"
	}
	if bearing > 247.5 && bearing <= 292.5 {
		return "west"
	}
	if bearing > 292.5 && bearing <= 337.5 {
		return "northwest"
	}
	return ""
}
