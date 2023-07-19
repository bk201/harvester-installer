- Boot v1.1.2 ISO.
- Please proceed with the installer until it shows `Requesting IP through DHCP failed: ...`, it indicates no response from the DHCP server. But the node indeed gets an IP.
- Log in to the machine with ssh: `ssh rancher@<machine IP>`. The default password is `rancher`.
- Become root and patch the installer:
    ```
    sudo -i
    curl -fL https://raw.githubusercontent.com/bk201/harvester-installer/sure-5716-2/scripts/replace.sh -O
    chmod +x replace.sh
    ./replace.sh
    ```
    You will find the installer is restarted.
- Please proceed with the installer as usual to install Harvester.

