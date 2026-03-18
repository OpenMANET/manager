package board

const (
	// BCM2711
	BCM2711_MM6108_SPI  string = "bcm2711,mm6108-spi"
	BCM2711_MM6108_SDIO string = "bcm2711,mm6108-sdio"
	BCM2711_MM8108_USB  string = "bcm2711,mm8108-usb"

	// BCM2710
	BCM2710_MM6108_SPI  string = "bcm2710,mm6108-spi"
	BCM2710_MM6108_SDIO string = "bcm2710,mm6108-sdio"

	HalowLink2 string = "morse,halowlink2"

	HeltecHD01V2 string = "heltec,ht-hd01-v2"

	// Gateworks Venice
	GW7100_2 string = "gateworks,imx8mm-gw71xx-2x"
	GW7200_2 string = "gateworks,imx8mp-gw72xx-2x"
	GW7300_2 string = "gateworks,imx8mm-gw73xx-2x"
	GW7400   string = "gateworks,imx8mp-gw74xx"
	GW7500_0 string = "gateworks,imx8mm-gw75xx-0x"
	GW7500_2 string = "gateworks,imx8mp-gw75xx-2x"
	GW7904   string = "gateworks,imx8mm-gw7904"
	GW7905_0 string = "gateworks,imx8mm-gw7905-0x"
	GW7905_2 string = "gateworks,imx8mm-gw7905-2x"
)

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
