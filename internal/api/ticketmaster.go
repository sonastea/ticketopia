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

func fetchEvents(ctx context.Context, logger zerolog.Logger, r *redis.Client) (home.Events, error) {
	var data home.Events
	cacheKey := fmt.Sprintf("events:%s", KEY)

	cachedData, err := r.Get(ctx, cacheKey).Result()
	if err == redis.Nil {
		res, err := http.Get(fmt.Sprintf("%s?apikey=%s", ROOT_URL, KEY))
		if err != nil {
			logger.Error().Msg("Error fetching events from discovery api.")
			return nil, err
		}

    var dataRes models.EventsResponse
		if err := json.NewDecoder(res.Body).Decode(&dataRes); err != nil {
			logger.Error().Msg("Error decoding fetch events response.")
		}

    groupedEvents := groupEventByName(dataRes)

		serializedData, err := json.Marshal(groupedEvents)
		if err != nil {
			logger.Error().Msg("Error marshalling fetched events response data.")
		}

		r.Set(ctx, cacheKey, serializedData, time.Hour)

		return groupedEvents, nil
	}

	// Cache hit
	if err := json.Unmarshal([]byte(cachedData), &data); err != nil {
		return nil, err
	}

	return data, nil
}

func (a *api) retrieveEventsHandler(c echo.Context) error {
	data, err := fetchEvents(c.Request().Context(), a.logger, a.redis)
	if err != nil {
		a.logger.Error().Msg("Error fetching events.")
		return nil
	}

	return render(c, http.StatusOK, home.Index(data))
}

func groupEventByName(data models.EventsResponse) home.Events {
	groups := make(home.Events)
	for _, v := range data.Embedded.Events {
		groups[v.Name] = append(groups[v.Name], v)
	}

	return groups
}
