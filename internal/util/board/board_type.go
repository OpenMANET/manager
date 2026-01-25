package board

type Board struct {
	Model   Model   `json:"model"`
	System  System  `json:"system"`
	Network Network `json:"network"`
	Wlan    struct {
		Phy0 struct {
			Path string `json:"path"`
			Info struct {
				Radios    []interface{} `json:"radios"`
				Bands     Bands         `json:"bands"`
				AntennaRx int           `json:"antenna_rx"`
				AntennaTx int           `json:"antenna_tx"`
			} `json:"info"`
		} `json:"phy0"`
		Phy1 struct {
			Path string `json:"path"`
			Info struct {
				Radios    []interface{} `json:"radios"`
				Bands     Bands         `json:"bands"`
				AntennaRx int           `json:"antenna_rx"`
				AntennaTx int           `json:"antenna_tx"`
			} `json:"info"`
		} `json:"phy1"`
		Phy2 struct {
			Path string `json:"path"`
			Info struct {
				Radios    []interface{} `json:"radios"`
				Bands     Bands         `json:"bands"`
				AntennaRx int           `json:"antenna_rx"`
				AntennaTx int           `json:"antenna_tx"`
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
		Modes          []string `json:"modes,omitempty"`
		MaxWidth       int      `json:"max_width,omitempty"`
		DefaultChannel int      `json:"default_channel,omitempty"`
		Ht             bool     `json:"ht,omitempty"`
		He             bool     `json:"he,omitempty"`
	} `json:"2G"`
	FiveG struct {
		Modes          []string `json:"modes,omitempty"`
		MaxWidth       int      `json:"max_width,omitempty"`
		DefaultChannel int      `json:"default_channel,omitempty"`
		Ht             bool     `json:"ht,omitempty"`
		Vht            bool     `json:"vht,omitempty"`
		He             bool     `json:"he,omitempty"`
	} `json:"5G"`
	SixG struct {
		Modes          []string `json:"modes,omitempty"`
		MaxWidth       int      `json:"max_width,omitempty"`
		DefaultChannel int      `json:"default_channel,omitempty"`
		He             bool     `json:"he,omitempty"`
	} `json:"6G"`
}
