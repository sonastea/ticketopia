package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/views/home"
)

func fetchEvents(ctx context.Context, page string, logger zerolog.Logger, r *redis.Client) (home.Events, error) {
	var data home.Events
	cacheKey := fmt.Sprintf("events:%s", page)

	cachedData, err := r.Get(ctx, cacheKey).Result()
	if err == redis.Nil {
		res, err := http.Get(fmt.Sprintf("%s?classificationName=music&size=%s&page=%s&apikey=%s", ROOT_URL, SIZE, page, KEY))
		if err != nil {
			logger.Error().Msg("Error fetching events from discovery api.")
			return nil, err
		}

		var dataRes models.EventsResponse
		if err := json.NewDecoder(res.Body).Decode(&dataRes); err != nil {
			logger.Error().Msg("Error decoding fetch events response.")
		}

		events := groupEventByName(dataRes)

		serializedData, err := json.Marshal(events)
		if err != nil {
			logger.Error().Msg("Error marshalling fetched events response data.")
		}

		r.Set(ctx, cacheKey, serializedData, time.Hour)

		return events, nil
	}

	// Cache hit
	if err := json.Unmarshal([]byte(cachedData), &data); err != nil {
		return nil, err
	}

	return data, nil
}

func (a *api) retrieveEventsHandler(c echo.Context) error {
	page := c.QueryParam("page")
	if page == "" {
		page = "1"
	}

	data, err := fetchEvents(c.Request().Context(), page, a.logger, a.redis)
	if err != nil {
		a.logger.Error().Msg("Error fetching events.")
		return nil
	}

	nextPage, _ := strconv.Atoi(page)
	nextPage++

	if c.Request().Header.Get("HX-Request") != "" {
		return render(c, http.StatusOK, home.MoreEventsList(data, fmt.Sprintf("%d", nextPage)))
	}

	return render(c, http.StatusOK, home.Index(data, fmt.Sprintf("%d", nextPage)))
}

func groupEventByName(data models.EventsResponse) home.Events {
	groups := make(home.Events)
	for _, v := range data.Embedded.Events {
		groups[v.Name] = append(groups[v.Name], v)
	}

	return groups
}
