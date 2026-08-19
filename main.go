package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/redis/go-redis/v9"
)

type config struct {
	WheatherKeyAPI string `env:"WHEATHER_API"`
	RedisAddr      string `env:"REDIS_ADDR"`
	RedisUsername  string `env:"REDIS_USERNAME"`
	RedisPassword  string `env:"REDIS_PASSWORD"`
}

type WeatherResponse struct {
	ResolvedAddress string `json:"resolvedAddress"`
	Timezone        string `json:"timezone"`
	Days            []Day  `json:"days"`
}

type Day struct {
	Date       string  `json:"datetime"`
	TempMax    float64 `json:"tempmax"`
	TempMin    float64 `json:"tempmin"`
	Temp       float64 `json:"temp"`
	Humidity   float64 `json:"humidity"`
	Precip     float64 `json:"precip"`
	WindSpeed  float64 `json:"windspeed"`
	Conditions string  `json:"conditions"`
	Icon       string  `json:"icon"`
}

type WheatherAPI struct {
	redis *redis.Client
	ctx   context.Context
	api   string
}

func (w_api *WheatherAPI) fetchFromVisualCrossing(city string) ([]byte, error) {
	url := fmt.Sprintf(
		"https://weather.visualcrossing.com/VisualCrossingWebServices/rest/services/timeline/%s?key=SNFTSZLYHJ54NBY4HDK8U2DAS",
		city,
	)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("visual crossing returned error %s\n", resp.StatusCode)
	}
	var wheather_response WeatherResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &wheather_response); err != nil {
		return nil, err
	}
	cleaned, err := json.Marshal(wheather_response)
	if err != nil {
		return nil, err
	}
	return cleaned, nil
}

func (w_api *WheatherAPI) getWheather(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		log.Println("Invalid http method")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	city := r.URL.Query().Get("city")
	if city == "" {
		log.Println("No `city` query parameter")
		http.Error(w, "Missing 'city' query parameter", http.StatusBadRequest)
		return
	}
	// Проверяем кэш
	cached, err := w_api.redis.Get(w_api.ctx, city).Result()
	if err == nil {
		log.Println("City found in cache")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.Write([]byte(cached))
		return
	}
	if err != redis.Nil {
		log.Printf("Redis error: %s\n", err)
	}
	// Кэша нет — идём во внешний API
	resp, err := w_api.fetchFromVisualCrossing(city)
	if err != nil {
		log.Printf("Error API fetch %s\n", err)
		http.Error(w, "Error to fetch wheather data", http.StatusBadGateway)
		return
	}
	if err := w_api.redis.Set(w_api.ctx, city, resp, time.Hour*12).Err(); err != nil {
		log.Printf("Error to cache fetched data: %s\n", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")
	w.Write(resp)
}

func main() {
	var cfg config
	err := env.Parse(&cfg)
	if err != nil {
		panic("Error to parse config")
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Username: cfg.RedisUsername,
		Password: cfg.RedisPassword,
		Protocol: 3,
	})
	w_api := WheatherAPI{
		redis: client,
		ctx:   ctx,
		api:   cfg.WheatherKeyAPI,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/wheather", w_api.getWheather)
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalln("Error to start listening")
	}
}
