package main

// Nagios service status bits (used by the servicestatustypes filter).
const (
	StatusPending  = 1
	StatusOK       = 2
	StatusWarning  = 4
	StatusUnknown  = 8
	StatusCritical = 16
)

// Nagios service property flag bits (used by the serviceprops filter).
const (
	ServiceScheduledDowntime    = 1
	ServiceNoScheduledDowntime  = 2
	ServiceStateAcknowledged    = 4
	ServiceStateUnacknowledged  = 8
	ServiceChecksEnabled        = 32
	ServiceIsNotFlapping        = 2048
	ServiceNotificationsEnabled = 8192
	ServiceActiveCheck          = 131072
	ServiceHardState            = 262144
)

// Nagios host property flag bits (used by the hostprops filter).
const (
	HostScheduledDowntime    = 1
	HostNoScheduledDowntime  = 2
	HostChecksEnabled        = 32
	HostNotificationsEnabled = 8192
	HostActiveCheck          = 131072
	HostHardState            = 262144
)

// Zabbix event.acknowledge action bits.
const (
	ZbxActionAcknowledge = 2
	ZbxActionAddMessage  = 4
	ZbxActionUnack       = 16
)

// Service is one nagios "host+service" row, built from a Zabbix problem.
type Service struct {
	EventID         string
	Hostname        string
	Service         string
	Status          int
	Flags           int
	HostFlags       int
	LastStateChange int64
	DurationSecs    int64
	PluginOutput    string
	HostAlive       int
	ServicesTotal   int
	ServicesVisible int
}
