#!/bin/bash

# Simple deployment script - just copies files, no passwords
VM_IP=192.168.4.79

echo "Building ublk-mem..."
go build -o ublk-mem ./cmd/ublk-mem

echo ""
echo "Files ready to copy:"
echo "  ./ublk-mem        - Main binary"  
echo "  ./test-vm.sh      - VM test script"
echo ""
echo "Manual deployment steps:"
echo "1. SSH to VM: ssh behrlich@$VM_IP"
echo "2. Create test directory: mkdir -p ~/ublk-test"
echo "3. Exit VM"
echo "4. Copy files: scp ublk-mem test-vm.sh behrlich@$VM_IP:~/ublk-test/"
echo "5. SSH back to VM: ssh behrlich@$VM_IP"
echo "6. Run test: cd ~/ublk-test && ./test-vm.sh"
echo ""
echo "Or run this one-liner (will prompt for password):"
echo "scp ublk-mem test-vm.sh behrlich@$VM_IP:~/ublk-test/"