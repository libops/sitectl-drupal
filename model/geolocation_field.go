package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type GeoLocationField []GeoLocation
type GeoLocation struct {
	Latitude     float32 `json:"lat"`
	Longitude    float32 `json:"lng"`
	LatitudeSin  float32 `json:"lat_sin"`
	LatitudeCos  float32 `json:"lat_cos"`
	LongitudeRad float32 `json:"lng_rad"`
	Data         string  `json:"data"`
}

func (field GeoLocationField) MarshalCSV() (string, error) {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|"), nil
}

func (field *GeoLocationField) UnmarshalCSV(csv string) error {
	values := strings.Split(csv, "|")
	s := make([]GeoLocation, len(values))
	for i, value := range values {
		parts := strings.Split(value, ", ")
		if len(parts) != 2 {
			return errors.New("invalid CSV format for GeoLocationField")
		}

		lat, err := strconv.ParseFloat(parts[0], 32)
		if err != nil {
			return fmt.Errorf("invalid latitude value: %v", err)
		}

		lng, err := strconv.ParseFloat(parts[1], 32)
		if err != nil {
			return fmt.Errorf("invalid longitude value: %v", err)
		}
		s[i] = GeoLocation{
			Latitude:  float32(lat),
			Longitude: float32(lng),
		}
	}
	return nil
}

func (field *GeoLocation) String() string {
	return fmt.Sprintf("%g, %g", field.Latitude, field.Longitude)
}
