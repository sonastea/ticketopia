package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/views/home"
)

func fetchEvents(ctx context.Context, page string, logger zerolog.Logger, cache kv.Store) (home.Events, error) {
	var data home.Events
	cacheKey := fmt.Sprintf("events:%s", page)

	cachedData, err := cache.Get(ctx, cacheKey)
	if err == nil {
		if err := json.Unmarshal(cachedData, &data); err == nil {
			return data, nil
		} else {
			logger.Warn().Err(err).Msg("Invalid cached events; fetching fresh events")
		}
	} else if !errors.Is(err, kv.ErrNotFound) {
		logger.Warn().Err(err).Msg("Could not read cached events; fetching fresh events")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s?classificationName=music&size=%s&page=%s&apikey=%s", ROOT_URL, SIZE, page, KEY), nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery API returned %s", res.Status)
	}

	var dataRes models.EventsResponse
	if err := json.NewDecoder(res.Body).Decode(&dataRes); err != nil {
		return nil, err
	}
	events := groupEventByName(dataRes)

	serializedData, err := json.Marshal(events)
	if err != nil {
		logger.Warn().Err(err).Msg("Could not serialize events for caching")
	} else if err := cache.Set(ctx, cacheKey, serializedData, time.Hour); err != nil {
		logger.Warn().Err(err).Msg("Could not cache events")
	}

	return events, nil
}

func (a *api) retrieveEventsHandler(c echo.Context) error {
	page := c.QueryParam("page")
	if page == "" {
		page = "1"
	}

	data, err := fetchEvents(c.Request().Context(), page, a.logger, a.cache)
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
