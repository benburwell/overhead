#!/bin/sh

if [ "$OVERHEAD_HOST" = "" ]; then
  echo "Please set \$OVERHEAD_HOST"
  exit 1
fi

# Go binary + config file
GOOS=linux GOARCH=arm64 go build
scp overhead $OVERHEAD_HOST:~/overhead
scp overhead.toml $OVERHEAD_HOST:~/overhead.toml.sample

# Python LCD stuff
scp i2c_driver.py $OVERHEAD_HOST:~/i2c_driver.py
scp lcd_server.py $OVERHEAD_HOST:~/lcd_server.py
