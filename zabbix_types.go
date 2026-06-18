package main

// Problem is a normalized Zabbix active problem.
type Problem struct {
	EventID      string
	TriggerID    string
	Name         string
	Severity     int
	Clock        int64
	Acknowledged bool
	Suppressed   bool
	Opdata       string
}
