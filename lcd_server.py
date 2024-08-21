from http.server import HTTPServer, BaseHTTPRequestHandler
import json

import i2c_driver


HTTP_PORT=8000


def handler_for_screen(screen):

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            content_len = int(self.headers.get('content-length'))
            body_bytes = self.rfile.read(content_len)
            body = json.loads(body_bytes)
            print('POST body', body)
            (ident, actype) = (body['Ident'], body['AircraftType'])
            screen.lcd_display_string(f"{ident} ({actype})")
            (orig, dest) = (body['Origin'], body['Destination'])
            screen.lcd_display_string(f"{orig}\0{dest}", 2)
            self.send_response(200)

    return Handler


def main():
    screen = i2c_driver.lcd()
    screen.lcd_load_custom_chars([
        [
            0b00100,
            0b00100,
            0b10110,
            0b11111,
            0b10110,
            0b00100,
            0b00100,
            0b00000
        ]
    ])

    server_address = ("localhost", HTTP_PORT)
    server = HTTPServer(server_address, handler_for_screen(screen))
    print(f"Starting HTTP server on port {HTTP_PORT}")
    server.serve_forever()


if __name__ == "__main__":
    main()
