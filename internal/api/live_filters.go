package api

import (
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
)

func liveQuery(c echo.Context, stream bool) (activity.Filters, []string, error) {
	f := activity.DefaultPreferences().Filters
	invalid := &accounts.ValidationError{Field: "filter", Message: "Use a public location/category/artist/venue filter of at most 4 KiB."}
	if len(c.Request().URL.RawQuery) > 32768 {
		return f, nil, invalid
	}
	q, err := url.ParseQuery(c.Request().URL.RawQuery)
	if err != nil {
		return f, nil, invalid
	}
	if scope, ok := c.Get("live.scope").(privateLiveScope); ok {
		f = scope.preferences.Filters
	} else if values, ok := q["filter"]; ok {
		if len(values) != 1 {
			return f, nil, invalid
		}
		f, err = activity.DecodePublicFilters([]byte(values[0]))
		if err != nil {
			return f, nil, invalid
		}
		q.Del("filter")
	}
	ids, err := activityIDs(q.Encode(), false, stream)
	return f, ids, err
}
