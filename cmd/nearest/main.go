package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/benburwell/firehose"
	lcd "github.com/d2r2/go-hd44780"
	"github.com/d2r2/go-i2c"
	"github.com/skypies/geo"
	"github.com/spf13/cast"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	FT_PER_NM = 6080.0
)

func main() {
	pflag.String("username", "", "Username for Firehose authentication")
	pflag.String("password", "", "Password for Firehose authentication")
	pflag.Float64("ceiling", 10000, "Maximum altitude in feet at which to display flights")
	pflag.Float64("radius", 3, "Radius in nautical miles around location within which to display flights")
	pflag.Duration("persist", time.Minute, "Persist flight on display for at most this long")
	pflag.Int("i2c-bus", 1, "I2C bus to use for LCD")
	pflag.Uint8("i2c-address", 0x27, "I2C address for LCD")
	configFile := pflag.StringP("config-file", "c", "", "Config file name")
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
		viper.AddConfigPath("$HOME/.config/nearest/")
		viper.SetConfigName("nearest")
		viper.AddConfigPath(".")
	}
	if err := viper.ReadInConfig(); err != nil {
		log.Fatal(err.Error())
	}

	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		log.Fatal(err.Error())
	}

	app := &App{
		Username:   viper.GetString("username"),
		Password:   viper.GetString("password"),
		Latitude:   viper.GetFloat64("latitude"),
		Longitude:  viper.GetFloat64("longitude"),
		RadiusNM:   viper.GetFloat64("radius"),
		CeilingFt:  viper.GetFloat64("ceiling"),
		I2CBus:     viper.GetInt("i2c-bus"),
		I2CAddress: cast.ToUint8(viper.Get("i2c-address")),
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type App struct {
	Username   string
	Password   string
	Latitude   float64
	Longitude  float64
	RadiusNM   float64
	CeilingFt  float64
	I2CBus     int
	I2CAddress uint8
}

func (a *App) Run(ctx context.Context) error {
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
		LatLong:  []firehose.Rectangle{a.flightObservationBox()},
	}

	if err := stream.Init(cmd.String()); err != nil {
		return fmt.Errorf("could not initialize firehose: %w", err)
	}

	screen, err := a.setupLCD()
	if err != nil {
		return err
	}

	positions := make(chan Position)
	defer close(positions)
	go renderPositions(positions, screen)

	for {
		msg, err := stream.NextMessage(ctx)
		if errors.Is(err, context.Canceled) {
			return nil
		} else if err != nil {
			return err
		}
		switch m := msg.Payload.(type) {
		case firehose.PositionMessage:
			pos, err := a.newPosition(&m)
			if err != nil {
				log.Println(err)
			} else {
				positions <- *pos
			}
		case firehose.ErrorMessage:
			return fmt.Errorf("firehose error: %s", m.ErrorMessage)
		}
	}
}

func (a *App) setupLCD() (*lcd.Lcd, error) {
	bus, err := i2c.NewI2C(a.I2CAddress, a.I2CBus)
	if err != nil {
		return nil, err
	}
	return lcd.NewLcd(bus, lcd.LCD_16x2)
}

func (a *App) flightObservationBox() firehose.Rectangle {
	center := a.myLocation()
	minLat := center.MoveNM(180, a.RadiusNM)
	maxLat := center.MoveNM(0, a.RadiusNM)
	minLon := center.MoveNM(270, a.RadiusNM)
	maxLon := center.MoveNM(90, a.RadiusNM)
	return firehose.Rectangle{
		LowLat: minLat.Lat,
		LowLon: minLon.Long,
		HiLat:  maxLat.Lat,
		HiLon:  maxLon.Long,
	}
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
	Distance     float64
	Bearing      float64
}

func (a *App) newPosition(msg *firehose.PositionMessage) (*Position, error) {
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
	pos.Distance = pos.Point.DistNM(a.myLocation())
	pos.Bearing = a.myLocation().BearingTowards(pos.Point)
	return &pos, nil
}

func (a *App) myLocation() geo.Latlong {
	return geo.Latlong{
		Lat:  a.Latitude,
		Long: a.Longitude,
	}
}

func cardinalDirection(bearing float64) string {
	if bearing > 337.5 || bearing <= 22.5 {
		return "N"
	}
	if bearing > 22.5 && bearing <= 67.5 {
		return "NE"
	}
	if bearing > 67.5 && bearing <= 112.5 {
		return "E"
	}
	if bearing > 112.5 && bearing <= 157.5 {
		return "SE"
	}
	if bearing > 157.5 && bearing <= 202.5 {
		return "S"
	}
	if bearing > 202.5 && bearing <= 247.5 {
		return "SW"
	}
	if bearing > 247.5 && bearing <= 292.5 {
		return "W"
	}
	if bearing > 292.5 && bearing <= 337.5 {
		return "NW"
	}
	return ""
}

func renderPositions(positions <-chan Position, screen *lcd.Lcd) {
	var position *Position

	refresh := time.NewTicker(5 * time.Second)
	defer refresh.Stop()

	var flip bool

	for {
		select {
		case <-refresh.C:
			flip = !flip

			if position != nil {
				// If our position is super old, turn the screen off.
				if time.Now().Sub(position.Timestamp) > time.Minute {
					position = nil
					screen.Clear()
					screen.BacklightOff()
					continue
				}

				// Otherwise, show the appropriate display.
				if flip {
					renderFlip(*position, screen)
				} else {
					renderFlop(*position, screen)
				}
			}
		case p := <-positions:
			if shouldReplace(position, &p) {
				position = &p
			}
		}
	}
}

func shouldReplace(prev, curr *Position) bool {
	// If we don't have a previous position at all, we should use the new one.
	if prev == nil {
		return true
	}

	// We (probably) have 3 sides of a right triangle. Convert down to consistent
	// units (feet), and fill in a default altitude for positions that don't have
	// one.
	prevDistFt, currDistFt := prev.Distance*FT_PER_NM, curr.Distance*FT_PER_NM
	prevAlt, currAlt := assumeAltitude(prev), assumeAltitude(curr)

	// Figure the 3D distance for each position.
	prevDist := math.Sqrt(prevDistFt*prevDistFt + prevAlt*prevAlt)
	currDist := math.Sqrt(currDistFt*currDistFt + currAlt*currAlt)

	if currDist < prevDist {
		return true
	}

	// Check if the old position is super old; we should replace it even if the
	// new one is further away.
	if time.Now().Sub(prev.Timestamp) > time.Minute {
		return true
	}

	return false
}

func assumeAltitude(p *Position) float64 {
	if p.Altitude != nil {
		return *p.Altitude
	}
	return 5000.0
}

func renderFlip(p Position, screen *lcd.Lcd) {
	screen.Clear()
	screen.ShowMessage(fmt.Sprintf("%s %s", p.Ident, p.AircraftType), lcd.SHOW_LINE_1|lcd.SHOW_BLANK_PADDING)
	var alt string
	if p.Altitude != nil {
		alt = fmt.Sprintf("%03.0f", *p.Altitude/100)
	}
	screen.ShowMessage(fmt.Sprintf("%1.1fnm %s %s", p.Distance, cardinalDirection(p.Bearing), alt), lcd.SHOW_LINE_2|lcd.SHOW_BLANK_PADDING)
	screen.BacklightOn()
}

func renderFlop(p Position, screen *lcd.Lcd) {
	if !isAirport(p.Origin) && !isAirport(p.Destination) {
		renderFlip(p, screen)
		return
	}

	screen.Clear()
	screen.ShowMessage(fmt.Sprintf("%s %s", p.Ident, p.AircraftType), lcd.SHOW_LINE_1|lcd.SHOW_BLANK_PADDING)

	orig, dest := p.Origin, p.Destination
	if !isAirport(orig) {
		orig = "????"
	}
	if !isAirport(dest) {
		dest = "????"
	}

	screen.ShowMessage(fmt.Sprintf("%s-%s", orig, dest), lcd.SHOW_LINE_2|lcd.SHOW_BLANK_PADDING)
	screen.BacklightOn()
}

// Check whether the given string is an airport. It needs to be non-blank and
// at most 4 characters long (ICAO aerodrome).
func isAirport(s string) bool {
	return s != "" && len(s) <= 4
}
