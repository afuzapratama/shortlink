package policy

// IPQualityFlags contains the boolean evidence returned by BEON-IPQuality.
type IPQualityFlags struct {
	Proxy      bool `json:"proxy"`
	VPN        bool `json:"vpn"`
	Tor        bool `json:"tor"`
	Datacenter bool `json:"datacenter"`
	Botnet     bool `json:"botnet"`
	Spam       bool `json:"spam"`
	Malware    bool `json:"malware"`
	Attacker   bool `json:"attacker"`
}

func (f IPQualityFlags) Values() map[string]bool {
	return map[string]bool{"proxy": f.Proxy, "vpn": f.VPN, "tor": f.Tor, "datacenter": f.Datacenter, "botnet": f.Botnet, "spam": f.Spam, "malware": f.Malware, "attacker": f.Attacker}
}
func ipQualityOutcome(r *LinkRouting, s Signals) (bool, string) {
	status := s.ProxyStatus
	if status == "" {
		status = "unavailable"
	}
	if r.IPQualityBlock == nil {
		return s.ProxyDetected, status
	} // Preserve published legacy choices.
	if status != "known" {
		return false, status
	}
	if s.IPQuality == nil {
		return false, "invalid_response"
	}
	values := s.IPQuality.Values()
	for _, name := range *r.IPQualityBlock {
		if values[name] {
			return true, status
		}
	}
	return false, status
}
