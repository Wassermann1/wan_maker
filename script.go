package main

var script = `
cat > /data/bin/rc.startup << 'EORCS'
#########################
####    VARIABLES    ####
#########################

LAN_ADDRESS=%s
LAN_NETMASK=255.255.255.0

LAN_DHCP_START=%s
LAN_DHCP_END=%s

#WAN_ADDRESS=185.215.108.95
#WAN_NETMASK=255.255.255.0
#WAN_GATEWAY=185.215.108.1
#WAN_DNS1=1.1.1.1
#WAN_DNS2=8.8.8.8


#####################
####    GUARD    ####
#####################

if [ -f /tmp/rc.startup ]; then
    exit 0
else
    :> /tmp/rc.startup
fi


################################
####    L2 CONFIGURATION    ####
################################

# Delete the default bridge

ip link delete dev br0


# Create a bridge

ip link add name br-lan type bridge


# Add interfaces to the bridge

ip link set dev lan2 master br-lan     # LAN2
ip link set dev lan3 master br-lan     # LAN3
ip link set dev lan4 master br-lan     # LAN4
ip link set dev ra0 master br-lan      # Wi-Fi 2.4G
ip link set dev eth0.6.0 master br-lan # Wi-Fi 5G

# Bring up the bridge

ip link set dev br-lan up


################################
####    L3 CONFIGURATION    ####
################################

# Assign IP addresses to the interfaces

if [ -n "$LAN_ADDRESS" ] && [ -n "$LAN_NETMASK" ]; then
    ip addr replace $LAN_ADDRESS/$LAN_NETMASK dev br-lan
fi

if [ -n "$WAN_ADDRESS" ] && [ -n "$WAN_NETMASK" ]; then
    ip addr replace $WAN_ADDRESS/$WAN_NETMASK dev lan1
fi


# Set the default gateway

if [ -n "$WAN_GATEWAY" ]; then
    ip route replace default via $WAN_GATEWAY dev lan1
fi


# Configure the firewall

ebtables -t filter -F
ebtables -t filter -X
ebtables -t nat -F
ebtables -t nat -X
ebtables -t broute -F
ebtables -t broute -X

iptables -t filter -F
iptables -t filter -X
iptables -t raw -F
iptables -t raw -X
iptables -t nat -F
iptables -t nat -X
iptables -t mangle -F
iptables -t mangle -X

iptables -t filter -P INPUT REJECT
iptables -t filter -P FORWARD REJECT
iptables -t filter -P OUTPUT ACCEPT

iptables -t filter -A INPUT -m conntrack --ctstate RELATED -j ACCEPT
iptables -t filter -A INPUT -m conntrack --ctstate ESTABLISHED -j ACCEPT
iptables -t filter -A INPUT -m conntrack --ctstate INVALID -j DROP

iptables -t filter -A INPUT -i lo -j ACCEPT

iptables -t filter -A INPUT -p icmp -i br-lan -j ACCEPT
iptables -t filter -A INPUT -p icmp -i lan1   -j ACCEPT

iptables -t filter -A INPUT -p tcp -i br-lan --dport 22 -j ACCEPT # SSH
iptables -t filter -A INPUT -p tcp -i br-lan --dport 53 -j ACCEPT # DNS
iptables -t filter -A INPUT -p udp -i br-lan --dport 53 -j ACCEPT # DNS
iptables -t filter -A INPUT -p udp -i br-lan --dport 67 -j ACCEPT # DHCP
iptables -t filter -A INPUT -p tcp -i br-lan --dport 80 -j ACCEPT # HTTP


iptables -t filter -A FORWARD -m conntrack --ctstate RELATED -j ACCEPT
iptables -t filter -A FORWARD -m conntrack --ctstate ESTABLISHED -j ACCEPT
iptables -t filter -A FORWARD -m conntrack --ctstate INVALID -j DROP

iptables -t filter -A FORWARD -i br-lan -o lan1 -j ACCEPT
iptables -t filter -A FORWARD -i br-lan -o eth0.6.10 -j ACCEPT


iptables -t nat -A POSTROUTING -o lan1 -j MASQUERADE


################################
####    L7 CONFIGURATION    ####
################################

# Configure DNS resolver

:> /etc/resolv.conf

if [ -n "$WAN_DNS1" ]; then
    echo "nameserver $WAN_DNS1" >> /etc/resolv.conf
fi

if [ -n "$WAN_DNS2" ]; then
    echo "nameserver $WAN_DNS2" >> /etc/resolv.conf
fi


# Configure DHCP client

cat > /tmp/udhcpc.script << 'EOF'
#!/opt/bin/busybox sh

if [ -n "$1" ]; then
    case "$1" in
        "deconfig")
            if [ -n "$interface" ]; then
                ip route flush dev $interface
                ip addr  flush dev $interface
            fi
        ;;
        
        "renew"|"bound")
            if [ -n "$interface" ]; then
                if [ -n "$ip" ] && [ -n "$subnet" ]; then
                    ip addr replace $ip/$subnet dev $interface
                fi

                if [ -n "$router" ]; then
                    ip route replace default via $router dev $interface
                fi

                if [ -n "$dns" ]; then
                    :> /etc/resolv.conf

                    for i in $dns; do
                        echo "nameserver $i" >> /etc/resolv.conf
                    done
                fi
            fi
        ;;
    esac
fi
EOF

/opt/bin/busybox chmod +x /tmp/udhcpc.script

if [ -z "$WAN_ADDRESS" ] || [ -z "$WAN_NETMASK" ]; then
    /opt/bin/busybox udhcpc -b -i lan1 -s /tmp/udhcpc.script
fi


# Configure DHCP server

cat > /tmp/udhcpd.conf << EOF
interface br-lan

start $LAN_DHCP_START
end   $LAN_DHCP_END

option subnet $LAN_NETMASK
option router $LAN_ADDRESS
option dns    $LAN_ADDRESS

lease_file /tmp/udhcpd.leases
EOF

killall dnsmasq
killall dhcp6s
killall radvd

/opt/bin/busybox touch  /tmp/udhcpd.leases
/opt/bin/busybox udhcpd /tmp/udhcpd.conf


# Configure DNS server

/opt/bin/dnsmasq -C /dev/null -u superadmin -i br-lan
EORCS
`
