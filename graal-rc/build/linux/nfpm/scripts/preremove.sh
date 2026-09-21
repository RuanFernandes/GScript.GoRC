#!/bin/bash

if command -v apparmor_parser >/dev/null 2>&1 && [ -f /etc/apparmor.d/graal-rc ]; then
  apparmor_parser -R /etc/apparmor.d/graal-rc >/dev/null 2>&1 || true
fi
