#!/bin/bash

# Deploy and test script for go-ublk
VM_IP=192.168.4.79
VM_USER=behrlich
VM_PATH=/home/behrlich/ublk-test
PASSWORD_FILE=/tmp/devvm_pwd.txt

set -e

if [ ! -f "$PASSWORD_FILE" ]; then
    echo "Error: Password file $PASSWORD_FILE not found"
    exit 1
fi

PASSWORD=$(cat "$PASSWORD_FILE" | tr -d '\n')

echo "Building go-ublk..."
go build -o ublk-mem ./cmd/ublk-mem

echo "Creating build directory..."
mkdir -p ./build
cp ublk-mem ./build/

echo "Deploying to VM at $VM_IP..."
sshpass -p "$PASSWORD" ssh -o StrictHostKeyChecking=no $VM_USER@$VM_IP "mkdir -p $VM_PATH"
sshpass -p "$PASSWORD" scp -o StrictHostKeyChecking=no ./build/* $VM_USER@$VM_IP:$VM_PATH/

echo "Checking kernel and ublk support on VM..."
sshpass -p "$PASSWORD" ssh -o StrictHostKeyChecking=no $VM_USER@$VM_IP << 'EOF'
echo "=== System Info ==="
uname -a
echo ""
echo "=== Checking ublk support ==="
if [ -e /dev/ublk-control ]; then
    echo "✓ /dev/ublk-control exists"
    ls -la /dev/ublk-control
else
    echo "✗ /dev/ublk-control not found"
    echo "Trying to load ublk_drv module..."
    if sudo modprobe ublk_drv 2>/dev/null; then
        echo "✓ ublk_drv module loaded successfully"
        if [ -e /dev/ublk-control ]; then
            echo "✓ /dev/ublk-control now exists"
            ls -la /dev/ublk-control
        else
            echo "✗ /dev/ublk-control still not found after loading module"
        fi
    else
        echo "✗ Failed to load ublk_drv module"
        echo "Checking if module is available..."
        if modinfo ublk_drv >/dev/null 2>&1; then
            echo "✓ ublk_drv module is available:"
            modinfo ublk_drv | head -5
        else
            echo "✗ ublk_drv module not available in kernel"
        fi
    fi
fi

echo ""
echo "=== Current modules ==="
lsmod | grep -i ublk || echo "No ublk modules loaded"
EOF

echo ""
echo "Ready to test! Run this to test the ublk-mem command:"
echo "sshpass -p \"\$(cat $PASSWORD_FILE)\" ssh $VM_USER@$VM_IP 'cd $VM_PATH && sudo ./ublk-mem --size=64M -v'"