package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/views/home"
)

func fetchEvents(ctx context.Context, logger zerolog.Logger, r *redis.Client) (*models.EventsResponse, error) {
  var data models.EventsResponse
	cacheKey := fmt.Sprintf("events:%s", KEY)

	cachedData, err := r.Get(ctx, cacheKey).Result()
	if err == redis.Nil {
		res, err := http.Get(fmt.Sprintf("%s?apikey=%s", ROOT_URL, KEY))
		if err != nil {
			logger.Error().Msg("Error fetching events from discovery api.")
			return nil, err
		}

		if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
			logger.Error().Msg("Error decoding fetch events response.")
		}

		serializedData, err := json.Marshal(data)
		if err != nil {
			logger.Error().Msg("Error marshalling fetched events response data.")
		}

		r.Set(ctx, cacheKey, serializedData, time.Hour)

		return &data, nil
	}

	// Cache hit
	if err := json.Unmarshal([]byte(cachedData), &data); err != nil {
		return nil, err
	}

	return &data, nil
}

func (a *api) retrieveEventsHandler(c echo.Context) error {
	data, err := fetchEvents(c.Request().Context(), a.logger, a.redis)
	if err != nil {
		a.logger.Error().Msg("Error fetching events.")
		return nil
	}

	return render(c, http.StatusOK, home.Index(data))
}
