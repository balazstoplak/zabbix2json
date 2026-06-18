package main

// Filter applies the nagios servicestatustypes/serviceprops/hostprops bitmasks
// and populates ServicesVisible (per host) across the surviving rows.
func Filter(services []Service, statusTypes, serviceProps, hostProps int) []Service {
	survivors := make([]Service, 0, len(services))
	for _, s := range services {
		if serviceProps != 0 && s.Flags&serviceProps != serviceProps {
			continue
		}
		if statusTypes != 0 && s.Status&statusTypes == 0 {
			continue
		}
		if hostProps != 0 && s.HostFlags&hostProps != hostProps {
			continue
		}
		survivors = append(survivors, s)
	}
	visible := make(map[string]int, len(survivors))
	for _, s := range survivors {
		visible[s.Hostname]++
	}
	for i := range survivors {
		survivors[i].ServicesVisible = visible[survivors[i].Hostname]
	}
	return survivors
}
