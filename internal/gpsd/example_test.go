package gpsd_test

import (
	"fmt"
	"time"

	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/rs/zerolog"
)

// ExampleNewGPSService demonstrates how to create a GPS service and monitor position updates.
func ExampleNewGPSService() {
	// Create a logger
	log := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()

	// Create GPS service - connects to GPSD at localhost:2947
	service, err := gpsd.NewGPSService(log)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create GPS service")
		return
	}
	defer service.Close()

	// The service automatically watches for TPV reports in the background
	// and updates the PositionReport field

	// Wait a moment for GPS data to arrive
	time.Sleep(2 * time.Second)

	// Get the current position at any time
	position := service.GetPositionReport()

	fmt.Printf("Current GPS Position:\n")
	fmt.Printf("  Mode: %d\n", position.Mode)
	fmt.Printf("  Latitude: %.6f\n", position.Lat)
	fmt.Printf("  Longitude: %.6f\n", position.Lon)
	fmt.Printf("  Altitude: %.2f m\n", position.Alt)
	fmt.Printf("  Speed: %.2f m/s\n", position.Speed)
	fmt.Printf("  Track: %.2f degrees\n", position.Track)
}

// ExampleNewGPSServiceWithAddress demonstrates connecting to GPSD at a custom address.
func ExampleNewGPSServiceWithAddress() {
	log := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()

	// Connect to GPSD at a custom address
	service, err := gpsd.NewGPSServiceWithAddress(log, "192.168.1.100:2947")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create GPS service")
		return
	}
	defer service.Close()

	// Monitor position in a loop
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for i := 0; i < 5; i++ {
		<-ticker.C
		position := service.GetPositionReport()

		if position.Mode >= gpsd.Mode2D {
			fmt.Printf("Position: %.6f, %.6f (mode=%d)\n",
				position.Lat, position.Lon, position.Mode)
		} else {
			fmt.Printf("No GPS fix yet (mode=%d)\n", position.Mode)
		}
	}
}

// ExampleGPSService_GetPositionReport demonstrates thread-safe position reading.
func ExampleGPSService_GetPositionReport() {
	log := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()

	service, err := gpsd.NewGPSService(log)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create GPS service")
		return
	}
	defer service.Close()

	// GetPositionReport is safe to call from multiple goroutines
	go func() {
		for i := 0; i < 10; i++ {
			position := service.GetPositionReport()
			fmt.Printf("Goroutine 1: %v\n", position.Class)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	go func() {
		for i := 0; i < 10; i++ {
			position := service.GetPositionReport()
			fmt.Printf("Goroutine 2: %v\n", position.Class)
			time.Sleep(150 * time.Millisecond)
		}
	}()

	time.Sleep(2 * time.Second)
}
