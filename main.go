package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/benburwell/firehose"
	"github.com/skypies/geo"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	CleanupAfter   = 10 * time.Minute
	WebhookTimeout = 10 * time.Second
)

func main() {
	pflag.String("username", "", "Username for Firehose authentication")
	pflag.String("password", "", "Password for Firehose authentication")
	pflag.Float64("interesting-radius", 10, "Radius in nautical miles around location to watch for flights")
	pflag.Float64("interesting-ceiling", 15000, "Maximum altitude in feet to watch for flights")
	pflag.Float64("alert-radius", 3, "Radius in nautical miles around location to alert on approaching flights")
	pflag.Bool("announce", false, "Aurally announce approaching aircraft")
	pflag.String("webhook-url", "", "URL to optionally send position updates to")
	configFile := pflag.StringP("config-file", "c", "overhead.toml", "Config file name")
	showHelp := pflag.BoolP("help", "h", false, "Show help")
	pflag.Parse()

	if *showHelp {
		pflag.Usage()
		os.Exit(0)
	}

	// If the user has specified a particular config file, read that one.
	if *configFile != "" {
		viper.SetConfigFile(*configFile)
	} else {
		// Otherwise, set the default config file base name
		viper.AddConfigPath("$HOME/.config/overhead/")
		viper.SetConfigName("overhead")
		viper.AddConfigPath(".")
	}
	if err := viper.ReadInConfig(); err != nil {
		log.Fatal(err.Error())
	}

	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		log.Fatal(err.Error())
	}

	app := &App{
		Username:             viper.GetString("username"),
		Password:             viper.GetString("password"),
		Latitude:             viper.GetFloat64("latitude"),
		Longitude:            viper.GetFloat64("longitude"),
		InterestingRadiusNM:  viper.GetFloat64("interesting-radius"),
		InterestingCeilingFt: viper.GetFloat64("interesting-ceiling"),
		AlertRadiusNM:        viper.GetFloat64("alert-radius"),
		Announce:             viper.GetBool("announce"),
		WebhookURL:           viper.GetString("webhook-url"),
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type App struct {
	Username             string
	Password             string
	Latitude             float64
	Longitude            float64
	InterestingRadiusNM  float64
	InterestingCeilingFt float64
	AlertRadiusNM        float64
	Announce             bool
	WebhookURL           string

	flights map[string]*Position
	// currentTime stores the most recently received clock
	currentTime time.Time
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

		a.cleanupStaleFlights()
	}
}

// cleanupStaleFlights removes any flights that have not been seen recently from the map.
func (a *App) cleanupStaleFlights() {
	for id, flight := range a.flights {
		// last heard + cleanup after < current time
		if flight.Timestamp.Add(CleanupAfter).Before(a.currentTime) {
			delete(a.flights, id)
		}
	}
}

func (a *App) flightObservationBox() firehose.Rectangle {
	center := a.myLocation()
	minLat := center.MoveNM(180, a.InterestingRadiusNM)
	maxLat := center.MoveNM(0, a.InterestingRadiusNM)
	minLon := center.MoveNM(270, a.InterestingRadiusNM)
	maxLon := center.MoveNM(90, a.InterestingRadiusNM)
	return firehose.Rectangle{
		LowLat: minLat.Lat,
		LowLon: minLon.Long,
		HiLat:  maxLat.Lat,
		HiLon:  maxLon.Long,
	}
}

func (a *App) isInteresting(pos *Position) bool {
	dist := pos.Point.DistNM(a.myLocation())
	if dist > a.InterestingRadiusNM {
		return false
	}
	if pos.Altitude != nil && *pos.Altitude > a.InterestingCeilingFt {
		return false
	}
	return true
}

type Position struct {
	FlightID     string
	Point        geo.Latlong
	Altitude     *float64
	Ident        string
	Reg          string
	Origin       string
	Destination  string
	AircraftType string
	Speed        *float64
	Heading      *float64
	Timestamp    time.Time
}

func newPosition(msg *firehose.PositionMessage) (*Position, error) {
	var pos Position
	pos.FlightID = msg.ID
	lat, err := strconv.ParseFloat(msg.Lat, 64)
	if err != nil {
		return nil, fmt.Errorf("lat: %w", err)
	}
	lon, err := strconv.ParseFloat(msg.Lon, 64)
	if err != nil {
		return nil, fmt.Errorf("lon: %w", err)
	}
	pos.Point = geo.Latlong{
		Lat:  lat,
		Long: lon,
	}
	if msg.Alt != "" {
		alt, err := strconv.ParseFloat(msg.Alt, 64)
		if err != nil {
			return nil, fmt.Errorf("alt: %w", err)
		}
		pos.Altitude = &alt
	}
	pos.Ident = msg.Ident
	pos.Reg = msg.Reg
	pos.Origin = msg.Orig
	pos.Destination = msg.Dest
	pos.AircraftType = msg.AircraftType
	if msg.GS != "" {
		gs, err := strconv.ParseFloat(msg.GS, 64)
		if err != nil {
			return nil, fmt.Errorf("gs: %w", err)
		}
		pos.Speed = &gs
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
		pos.Heading = &hdg
	}
	clock, err := strconv.ParseInt(msg.Clock, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("clock: %w", err)
	}
	pos.Timestamp = time.Unix(clock, 0)
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
	a.currentTime = curr.Timestamp
	if !a.isInteresting(curr) {
		return
	}

	if a.flights == nil {
		a.flights = make(map[string]*Position)
	}
	if prev, ok := a.flights[curr.FlightID]; ok {
		me := a.myLocation()
		distToPrev := prev.Point.DistNM(me)
		distToCurr := curr.Point.DistNM(me)
		if distToCurr < distToPrev && distToCurr < a.AlertRadiusNM {
			a.alert(curr)
		}
	}
	a.flights[curr.FlightID] = curr
}

func (a *App) alert(curr *Position) {
	go a.displayFlight(curr)
	go a.postWebhook(curr)
	go a.say(curr)
}

func (a *App) postWebhook(pos *Position) {
	if a.WebhookURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), WebhookTimeout)
	defer cancel()
	body, err := json.Marshal(pos)
	if err != nil {
		log.Println(err.Error())
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.WebhookURL, bytes.NewReader(body))
	if err != nil {
		log.Println(err.Error())
		return
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("user-agent", "overhead-webhook https://github.com/benburwell/overhead")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println(err.Error())
		return
	}
	log.Printf("sent webhook to %s and got HTTP response code %s", a.WebhookURL, res.Status)
}

func (a *App) displayFlight(curr *Position) {
	me := a.myLocation()
	dist := curr.Point.DistNM(me)
	bearing := me.BearingTowards(curr.Point)

	var alert strings.Builder

	alert.WriteString(fmt.Sprintf("[%s] ", curr.Timestamp.Format("15:04:05")))

	alert.WriteString(curr.Ident)
	if curr.AircraftType != "" {
		alert.WriteString(" (" + curr.AircraftType + ")")
	}
	alert.WriteString(" from " + curr.Origin)
	if curr.Destination != "" {
		alert.WriteString(" to " + curr.Destination)
	}
	alert.WriteString(fmt.Sprintf(" is %.1fnm to the %s", dist, cardinalDirection(bearing)))
	if curr.Altitude != nil {
		alert.WriteString(fmt.Sprintf(" at %.0fft", *curr.Altitude))
	}
	dir := "travelling"
	if curr.Heading != nil {
		dir = cardinalDirection(*curr.Heading) + "bound"
	}
	if curr.Speed != nil {
		alert.WriteString(fmt.Sprintf(" %s at %.0fkts", dir, *curr.Speed))
	}

	alert.WriteString(fmt.Sprintf("\n           https://www.flightaware.com/live/flight/id/%s", curr.FlightID))

	fmt.Println(alert.String())
}

func (a *App) say(curr *Position) {
	if !a.Announce {
		return
	}

	me := a.myLocation()
	dist := curr.Point.DistNM(me)
	bearing := me.BearingTowards(curr.Point)

	var words []string
	words = append(words, identToWords(curr.Ident)...)
	words = append(words, "is")
	words = append(words, phonetic(fmt.Sprintf("%.1f", dist))...)
	words = append(words, "nautical miles")
	words = append(words, "to the", cardinalDirection(bearing), ",")
	if curr.Altitude != nil {
		words = append(words, "at")
		words = append(words, altitudeToWords(*curr.Altitude)...)
		words = append(words, ",")
	}
	if curr.Heading != nil {
		words = append(words, cardinalDirection(*curr.Heading), "bound", ",")
	}
	if curr.Speed != nil {
		words = append(words, phonetic(fmt.Sprintf("%.0f", *curr.Speed))...)
		words = append(words, "knots")
	}
	alert := strings.Join(words, " ")

	if err := exec.Command("say", "-r", "200", alert).Run(); err != nil {
		log.Println(err.Error())
	}
}

func identToWords(ident string) []string {
	icaoRegex := regexp.MustCompile("^[A-Z]{3}")
	icao := icaoRegex.FindString(ident)
	if icao == "" {
		return phonetic(ident)
	}
	suffix := ident[3:]
	callsign := icaoCallsign(icao)
	if callsign == "" {
		return phonetic(ident)
	}

	words := []string{callsign}
	numberRegex := regexp.MustCompile("^[0-9]{2,4}$")
	if numberRegex.MatchString(suffix) {
		switch len(suffix) {
		case 2:
			words = append(words, suffix)
		case 3:
			words = append(words, suffix[0:1], suffix[1:])
		case 4:
			words = append(words, suffix[0:2], suffix[2:])
		}
	} else {
		words = append(words, phonetic(suffix)...)
	}
	return words
}

func icaoCallsign(icao string) string {
	callsigns := map[string]string{
		"UAL": "united",
		"FDX": "fedex",
		"DAL": "delta",
		"KAP": "cair",
		"NKS": "spirit",
		"RPA": "brickyard",
		"ACA": "air canada",
		"POE": "porter",
		"SWA": "southwest",
		"JBU": "jet blue",
		"EIN": "shamrock",
		"AAL": "american",
		"ASA": "alaska",
		"FFT": "frontier flight",
		"JAL": "japan air",
		"JZA": "jazz",
		"AFR": "air france",
		"FPY": "player",
		"WUP": "up jet",
		"BAW": "speed bird",
		"VJA": "vista am",
	}
	return callsigns[icao]
}

func altitudeToWords(altitude float64) []string {
	var words []string
	thousands := int(altitude) / 1000
	if thousands > 0 {
		words = append(words, phonetic(strconv.Itoa(thousands))...)
		words = append(words, "thousand")
	}
	hundreds := (int(altitude) - (thousands * 1000)) / 100
	if hundreds > 0 {
		words = append(words, phonetic(strconv.Itoa(hundreds))...)
		words = append(words, "hundred")
	}
	return words
}

func phonetic(plain string) []string {
	var words []string
	alphabet := map[rune]string{
		'A': "alpha",
		'B': "bravo",
		'C': "charlie",
		'D': "delta",
		'E': "echo",
		'F': "foxtrot",
		'G': "golf",
		'H': "hotel",
		'I': "india",
		'J': "juliet",
		'K': "kilo",
		'L': "lima",
		'M': "mike",
		'N': "november",
		'O': "oscar",
		'P': "papa",
		'Q': "quebec",
		'R': "romeo",
		'S': "sierra",
		'T': "tango",
		'U': "uniform",
		'V': "victor",
		'W': "whiskey",
		'X': "x-ray",
		'Y': "yankee",
		'Z': "zulu",
		'0': "zero",
		'1': "one",
		'2': "two",
		'3': "three",
		'4': "four",
		'5': "five",
		'6': "six",
		'7': "seven",
		'8': "eight",
		'9': "niner",
		'.': "point",
	}
	for _, r := range plain {
		word, ok := alphabet[r]
		if ok {
			words = append(words, word)
		} else {
			words = append(words, string(r))
		}
	}
	return words
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
