#!/bin/bash
# Script to generate the demo.gif for the README
# Requires: vhs, tmux

set -e

# CD to project root
cd "$(dirname "$0")/.."

echo "Setting up mock environment..."
mkdir -p docs/bin

cat << 'MOCK' > docs/bin/tubo
#!/bin/bash
printf "\033[?25l" # hide cursor
echo "Connecting..."
sleep 0.5
echo -e "\033[2A\033[KReady! Share this command:\n"
echo "tubo receive 9xz33a-35qd44o5-BzLnYdWzy4DdoLoI"
echo ""
echo "No Tubo installed? Use this instead:"
echo "curl -sL https://tubo.endlessite.com/run | sh -s receive 9xz33a-35qd44o5-BzLnYdWzy4DdoLoI"
echo ""
echo "Waiting for receiver... (Ctrl+C to cancel)"
sleep 6.9
echo -e "\033[1A\033[KReceiver connected! Sending file..."

for i in {1..50}; do
  printf "\r\033[KSending: %d%% | %d.0 MB / 25.0 MB" $((i*2)) $((i/2))
  sleep 0.05
done
echo ""
echo "Transfer complete. Checksum (SHA-256): f7e6495778d2ed40..."
printf "\033[?25h" # show cursor
MOCK
chmod +x docs/bin/tubo

cat << 'MOCK' > docs/bin/sh
#!/bin/bash
printf "\033[?25l" # hide cursor
echo "Connecting..."
sleep 0.5
echo -e "\033[1A\033[KIncoming file: secret.sql"
echo ""
echo "Receiving: 25.0 MB"
for i in {1..50}; do
  printf "\r\033[KReceiving: %d%% | %d.0 MB / 25.0 MB" $((i*2)) $((i/2))
  sleep 0.05
done
echo ""
echo "Checksum verified"
echo "Done! File saved to ./secret.sql"
printf "\033[?25h" # show cursor
MOCK
chmod +x docs/bin/sh

cat << 'MOCK' > docs/bin/curl
#!/bin/bash
exit 0
MOCK
chmod +x docs/bin/curl

echo "Running vhs to generate demo.gif..."
vhs < docs/demo.tape

echo "Cleaning up..."
rm -rf docs/bin

echo "Done! demo.gif has been updated."
