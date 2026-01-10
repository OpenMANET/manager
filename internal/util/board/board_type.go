package board

type Board struct {
	Model   Model   `json:"model"`
	System  System  `json:"system"`
	Network Network `json:"network"`
	Wlan    struct {
		Phy0 struct {
			Path string `json:"path"`
			Info struct {
				AntennaRx int           `json:"antenna_rx"`
				AntennaTx int           `json:"antenna_tx"`
				Bands     Bands         `json:"bands"`
				Radios    []interface{} `json:"radios"`
			} `json:"info"`
		} `json:"phy0"`
		Phy1 struct {
			Path string `json:"path"`
			Info struct {
				AntennaRx int           `json:"antenna_rx"`
				AntennaTx int           `json:"antenna_tx"`
				Bands     Bands         `json:"bands"`
				Radios    []interface{} `json:"radios"`
			} `json:"info"`
		} `json:"phy1"`
		Phy2 struct {
			Path string `json:"path"`
			Info struct {
				AntennaRx int           `json:"antenna_rx"`
				AntennaTx int           `json:"antenna_tx"`
				Bands     Bands         `json:"bands"`
				Radios    []interface{} `json:"radios"`
			} `json:"info"`
		} `json:"phy2"`
	} `json:"wlan"`
}

type Model struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type System struct {
	Hostname string `json:"hostname,omitempty"`
}

type Network struct {
	Lan Lan `json:"lan"`
}

type Lan struct {
	Device   string `json:"device,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Ipaddr   string `json:"ipaddr,omitempty"`
	Netmask  string `json:"netmask,omitempty"`
}

type Bands struct {
	TwoG struct {
		Ht             bool     `json:"ht,omitempty"`
		He             bool     `json:"he,omitempty"`
		MaxWidth       int      `json:"max_width,omitempty"`
		Modes          []string `json:"modes,omitempty"`
		DefaultChannel int      `json:"default_channel,omitempty"`
	} `json:"2G"`
	FiveG struct {
		Ht             bool     `json:"ht,omitempty"`
		Vht            bool     `json:"vht,omitempty"`
		He             bool     `json:"he,omitempty"`
		MaxWidth       int      `json:"max_width,omitempty"`
		Modes          []string `json:"modes,omitempty"`
		DefaultChannel int      `json:"default_channel,omitempty"`
	} `json:"5G"`
	SixG struct {
		He             bool     `json:"he,omitempty"`
		MaxWidth       int      `json:"max_width,omitempty"`
		Modes          []string `json:"modes,omitempty"`
		DefaultChannel int      `json:"default_channel,omitempty"`
	} `json:"6G"`
}
