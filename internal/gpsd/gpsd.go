// Package gpsd provides a client for connecting to and reading GPS data from a GPSD server.
// It automatically maintains a connection to GPSD, reads TPV (Time-Position-Velocity) reports,
// SKY (satellite) reports, and raw NMEA sentences, providing thread-safe access to GPS data.
//
// Example usage:
//
//	gps, err := gpsd.NewGPSService(log, cfg)
//	if err != nil {
//	    return err
//	}
//	defer gps.Close()
//
//	// Get current position from TPV reports
//	pos := gps.GetPosition()
//	if pos.Valid {
//	    fmt.Printf("Lat: %f, Lon: %f\n", pos.Latitude, pos.Longitude)
//	}
//
//	// Get self-generated NMEA formatted output
//	nmea := gps.ToNMEA()
//	if nmea != "" {
//	    fmt.Println(nmea)
//	}
//
//	// Get raw NMEA sentences directly from GPSD
//	gpggaSentence := gps.GetNMEASentence("GPGGA")
//	if gpggaSentence != "" {
//	    fmt.Println("Raw GPGGA:", gpggaSentence)
//	}
//
//	// Get all NMEA sentence types
//	allSentences := gps.GetAllNMEASentences()
//	for sentenceType, sentence := range allSentences {
//	    fmt.Printf("%s: %s\n", sentenceType, sentence)
//	}
package gpsd
