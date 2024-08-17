import os
import sys
import signal
import logging
import re
import time
from datetime import datetime, timedelta
import math
import requests
from geopy.distance import geodesic
from geopy.point import Point
from argparse import ArgumentParser
import toml

CLEANUP_AFTER = timedelta(minutes=10)


class App:
    def __init__(self, config):
        self.username = config["username"]
        self.password = config["password"]
        self.latitude = config["latitude"]
        self.longitude = config["longitude"]
        self.interesting_radius_nm = config["interesting_radius"]
        self.interesting_ceiling_ft = config["interesting_ceiling"]
        self.alert_radius_nm = config["alert_radius"]
        self.announce = config["announce"]
        self.flights = {}
        self.current_time = None

    def run(self):
        try:
            self._connect_to_firehose()
            while True:
                self._process_messages()
                self.cleanup_stale_flights()
        except KeyboardInterrupt:
            print("Interrupted")
        finally:
            self._disconnect_from_firehose()

    def _connect_to_firehose(self):
        # Implement Firehose connection setup
        pass

    def _disconnect_from_firehose(self):
        # Implement Firehose disconnection
        pass

    def _process_messages(self):
        # Fetch and process messages from the Firehose stream
        pass

    def cleanup_stale_flights(self):
        now = datetime.utcnow()
        self.flights = {
            k: v
            for k, v in self.flights.items()
            if v["timestamp"] + CLEANUP_AFTER > now
        }

    def flight_observation_box(self):
        center = self.my_location()
        min_lat = self.move_nm(center, 180, self.interesting_radius_nm)
        max_lat = self.move_nm(center, 0, self.interesting_radius_nm)
        min_lon = self.move_nm(center, 270, self.interesting_radius_nm)
        max_lon = self.move_nm(center, 90, self.interesting_radius_nm)
        return {
            "low_lat": min_lat.latitude,
            "low_lon": min_lon.longitude,
            "hi_lat": max_lat.latitude,
            "hi_lon": max_lon.longitude,
        }

    def is_interesting(self, pos):
        dist = self.calculate_distance(pos["point"])
        if dist > self.interesting_radius_nm:
            return False
        if pos["altitude"] and pos["altitude"] > self.interesting_ceiling_ft:
            return False
        return True

    def calculate_distance(self, point):
        my_location = self.my_location()
        return geodesic(my_location, point).nautical

    def my_location(self):
        return Point(self.latitude, self.longitude)

    def handle_position(self, msg):
        curr = self.new_position(msg)
        if not curr:
            logging.warning("could not translate position message")
            return

        self.current_time = curr["timestamp"]
        if not self.is_interesting(curr):
            return

        if prev := self.flights.get(curr["flight_id"]):
            dist_to_prev = self.calculate_distance(prev["point"])
            dist_to_curr = self.calculate_distance(curr["point"])
            if dist_to_curr < dist_to_prev and dist_to_curr < self.alert_radius_nm:
                self.alert(curr)

        self.flights[curr["flight_id"]] = curr

    def alert(self, curr):
        self.display_flight(curr)
        if self.announce:
            self.say(curr)

    def display_flight(self, curr):
        me = self.my_location()
        dist = self.calculate_distance(curr["point"])
        bearing = self.bearing_towards(curr["point"])
        alert = f"[{curr['timestamp'].strftime('%H:%M:%S')}] {curr['ident']} ({curr['aircraft_type']}) from {curr['origin']} to {curr['destination']} is {dist:.1f}nm to the {self.cardinal_direction(bearing)} at {curr['altitude']:.0f}ft travelling {self.cardinal_direction(curr['heading'])}bound at {curr['speed']:.0f}kts"
        print(alert)

    def say(self, curr):
        words = [
            *self.ident_to_words(curr["ident"]),
            "is",
            *self.phonetic(f"{self.calculate_distance(curr['point']):.1f}"),
            "nautical miles",
            "to the",
            self.cardinal_direction(self.bearing_towards(curr["point"])),
            ",",
        ]
        if curr["altitude"]:
            words += ["at", *self.altitude_to_words(curr["altitude"]), ","]
        if curr["heading"]:
            words += [self.cardinal_direction(curr["heading"]), "bound", ","]
        if curr["speed"]:
            words += [*self.phonetic(f"{curr['speed']:.0f}"), "knots"]
        alert = " ".join(words)
        os.system(f"say -r 200 '{alert}'")

    def new_position(self, msg):
        try:
            point = Point(float(msg["lat"]), float(msg["lon"]))
            altitude = float(msg["alt"]) if msg["alt"] else None
            speed = float(msg["gs"]) if msg["gs"] else None
            heading = (
                float(msg["heading_true"])
                if msg["heading_true"]
                else (float(msg["heading"]) if msg["heading"] else None)
            )
            timestamp = datetime.utcfromtimestamp(int(msg["clock"]))
            return {
                "flight_id": msg["id"],
                "point": point,
                "altitude": altitude,
                "ident": msg["ident"],
                "reg": msg["reg"],
                "origin": msg["orig"],
                "destination": msg["dest"],
                "aircraft_type": msg["aircraft_type"],
                "speed": speed,
                "heading": heading,
                "timestamp": timestamp,
            }
        except Exception as e:
            logging.error(f"Error parsing message: {e}")
            return None

    def bearing_towards(self, point):
        return geodesic(self.my_location(), point).initial

    def cardinal_direction(self, bearing):
        if bearing > 337.5 or bearing <= 22.5:
            return "north"
        if bearing > 22.5 and bearing <= 67.5:
            return "northeast"
        if bearing > 67.5 and bearing <= 112.5:
            return "east"
        if bearing > 112.5 and bearing <= 157.5:
            return "southeast"
        if bearing > 157.5 and bearing <= 202.5:
            return "south"
        if bearing > 202.5 and bearing <= 247.5:
            return "southwest"
        if bearing > 247.5 and bearing <= 292.5:
            return "west"
        if bearing > 292.5 and bearing <= 337.5:
            return "northwest"
        return ""

    def ident_to_words(self, ident):
        icao_regex = re.compile("^[A-Z]{3}")
        icao = icao_regex.match(ident)
        if not icao:
            return self.phonetic(ident)
        suffix = ident[3:]
        callsign = self.icao_callsign(icao.group(0))
        if not callsign:
            return self.phonetic(ident)

        words = [callsign]
        number_regex = re.compile("^[0-9]{2,4}$")
        if number_regex.match(suffix):
            if len(suffix) == 2:
                words.append(suffix)
            elif len(suffix) == 3:
                words.append(suffix[0:1])
                words.append(suffix[1:])
            elif len(suffix) == 4:
                words.append(suffix[0:2])
                words.append(suffix[2:])
        else:
            words += self.phonetic(suffix)
        return words

    def icao_callsign(self, icao):
        callsigns = {
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
        return callsigns.get(icao)

    def altitude_to_words(self, altitude):
        words = []
        thousands = int(altitude) // 1000
        if thousands > 0:
            words += self.phonetic(str(thousands))
            words.append("thousand")
        hundreds = (int(altitude) - (thousands * 1000)) // 100
        if hundreds > 0:
            words += self.phonetic(str(hundreds))
            words.append("hundred")
        return words

    def phonetic(self, plain):
        alphabet = {
            "A": "alpha",
            "B": "bravo",
            "C": "charlie",
            "D": "delta",
            "E": "echo",
            "F": "foxtrot",
            "G": "golf",
            "H": "hotel",
            "I": "india",
            "J": "juliet",
            "K": "kilo",
            "L": "lima",
            "M": "mike",
            "N": "november",
            "O": "oscar",
            "P": "papa",
            "Q": "quebec",
            "R": "romeo",
            "S": "sierra",
            "T": "tango",
            "U": "uniform",
            "V": "victor",
            "W": "whiskey",
            "X": "x-ray",
            "Y": "yankee",
            "Z": "zulu",
            "0": "zero",
            "1": "one",
            "2": "two",
            "3": "three",
            "4": "four",
            "5": "five",
            "6": "six",
            "7": "seven",
            "8": "eight",
            "9": "niner",
            ".": "point",
        }
        words = []
        for char in plain.upper():
            words.append(alphabet.get(char, char))
        return words

    def move_nm(self, point, bearing, distance_nm):
        """Moves the point a specified distance (in nautical miles) in the given bearing (degrees)."""
        # Using geodesic for simplicity; this can be more accurate using specific aviation libraries
        return geodesic(nautical=distance_nm).destination(point, bearing)


def load_config(config_file):
    with open(config_file, "r") as f:
        config = toml.load(f)
    return config


def main():
    parser = ArgumentParser(description="Aircraft tracker using Firehose stream.")
    parser.add_argument(
        "-u", "--username", type=str, help="Username for Firehose authentication"
    )
    parser.add_argument(
        "-p", "--password", type=str, help="Password for Firehose authentication"
    )
    parser.add_argument("--latitude", type=float, help="Your current latitude")
    parser.add_argument("--longitude", type=float, help="Your current longitude")
    parser.add_argument(
        "--interesting-radius",
        type=float,
        default=10.0,
        help="Radius in nautical miles to watch for flights",
    )
    parser.add_argument(
        "--interesting-ceiling",
        type=float,
        default=15000.0,
        help="Maximum altitude in feet to watch for flights",
    )
    parser.add_argument(
        "--alert-radius",
        type=float,
        default=3.0,
        help="Radius in nautical miles to alert on approaching flights",
    )
    parser.add_argument(
        "--announce", action="store_true", help="Announce approaching aircraft"
    )
    parser.add_argument(
        "-c",
        "--config-file",
        type=str,
        default="overhead.toml",
        help="Path to the configuration file",
    )

    args = parser.parse_args()

    # Load config
    config = load_config(args.config_file)

    # Overwrite config with command-line arguments if provided
    if args.username:
        config["username"] = args.username
    if args.password:
        config["password"] = args.password
    if args.latitude:
        config["latitude"] = args.latitude
    if args.longitude:
        config["longitude"] = args.longitude
    if args.interesting_radius:
        config["interesting_radius"] = args.interesting_radius
    if args.interesting_ceiling:
        config["interesting_ceiling"] = args.interesting_ceiling
    if args.alert_radius:
        config["alert_radius"] = args.alert_radius
    if args.announce:
        config["announce"] = args.announce

    # Initialize and run the application
    app = App(config)

    def handle_signal(sig, frame):
        sys.exit(0)

    signal.signal(signal.SIGINT, handle_signal)
    signal.signal(signal.SIGTERM, handle_signal)

    app.run()


if __name__ == "__main__":
    main()
