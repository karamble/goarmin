// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Prompt is a ready-made request an assistant offers its user (a slash command in many
// clients): what to look at, with which tools.
type Prompt struct {
	Name  string
	Title string
	Desc  string
	Args  []*mcp.PromptArgument
	// Text writes the request; level says what the token may do.
	Text func(args map[string]string, today string, l Level) (string, error)
}

func dayArg(desc string) *mcp.PromptArgument {
	return &mcp.PromptArgument{Name: "day", Title: "Day", Description: desc + " (YYYY-MM-DD; default today)"}
}

// day reads a day argument, today by default.
func day(args map[string]string, today string) (string, error) {
	d := strings.TrimSpace(args["day"])
	if d == "" {
		return today, nil
	}
	if _, err := time.Parse(time.DateOnly, d); err != nil {
		return "", errors.New("day must be YYYY-MM-DD")
	}
	return d, nil
}

func shift(d string, n int) string {
	t, _ := time.Parse(time.DateOnly, d)
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

// Prompts are the ready-made requests.
var Prompts = []Prompt{
	{Name: "review_week", Title: "Review my week", Desc: "A look back at seven days: training, sleep, recovery and how the plan went.",
		Args: []*mcp.PromptArgument{dayArg("the week's last day")},
		Text: func(a map[string]string, today string, _ Level) (string, error) {
			to, err := day(a, today)
			if err != nil {
				return "", err
			}
			from := shift(to, -6)
			return fmt.Sprintf(`Review my week from %[1]s to %[2]s in goarmin.

1. get_calendar from=%[1]s to=%[2]s for the days, activities and plans (done, missed, ahead).
2. list_activities from_day=%[1]s to_day=%[2]s; get_activity for the ones that stand out.
3. get_daily from=%[1]s to=%[2]s for steps, stress, Body Battery, resting heart rate, intensity minutes, training readiness and load.
4. list_nights from=%[1]s to=%[2]s for sleep scores and stages.

Then tell me in plain words: what I did, how it compares with the plan, how I slept and recovered, and one or two things worth changing next week. Use the numbers, but keep it short.`, from, to), nil
		}},
	{Name: "review_night", Title: "Last night's sleep", Desc: "A night of sleep explained: score, stages, heart rate, HRV and what affected it.",
		Args: []*mcp.PromptArgument{dayArg("the day the night ended")},
		Text: func(a map[string]string, today string, _ Level) (string, error) {
			d, err := day(a, today)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(`Explain my sleep for the night that ended on %[1]s.

1. list_nights from=%[2]s to=%[1]s and take the night ending on %[1]s.
2. get_night with its start for the stages and the heart rate, HRV, respiration and Body Battery during it.
3. get_daily from=%[2]s to=%[1]s for the evening before (stress, activities) and the HRV status.

Tell me the score and what drove it (duration, deep and REM sleep, awake time, restlessness), how heart rate and HRV behaved, and whether anything the day before likely affected it. Keep it short.`, d, shift(d, -1)), nil
		}},
	{Name: "plan_week", Title: "Plan my week", Desc: "Suggest a week of workouts from the library for a goal, and put them on the calendar once you agree.",
		Args: []*mcp.PromptArgument{
			{Name: "goal", Title: "Goal", Description: "what the week is for, e.g. \"base building, 4 runs\"", Required: true},
			{Name: "day", Title: "First day", Description: "the week's first day (YYYY-MM-DD; default today)"},
		},
		Text: func(a map[string]string, today string, l Level) (string, error) {
			from, err := day(a, today)
			if err != nil {
				return "", err
			}
			goal := strings.TrimSpace(a["goal"])
			if goal == "" {
				return "", errors.New("goal is needed")
			}
			to := shift(from, 6)
			write := "Ask me before planning anything. Once I agree, put each workout on its day with plan_workout (day, workout_id, a short note), then show me get_calendar for the week."
			if l < Write {
				write = "This token can only read, so give me the plan as a list I can put on the calendar myself."
			}
			return fmt.Sprintf(`Help me plan %[1]s to %[2]s. My goal: %[3]s.

1. list_workouts for the library (and get_workout for promising ones).
2. get_calendar from=%[1]s to=%[2]s for what is planned already, including Garmin Coach.
3. get_daily from=%[4]s to=%[5]s for my recent load, readiness and recovery, and list_activities from_day=%[4]s to_day=%[5]s for what I did lately.

Suggest one workout or a rest day per day, with a reason each, balancing hard and easy days against my recent load. %[6]s`, from, to, goal, shift(from, -28), shift(from, -1), write), nil
		}},
	{Name: "explain_day", Title: "Explain a day", Desc: "Everything goarmin knows about one day, in plain words.",
		Args: []*mcp.PromptArgument{dayArg("the day")},
		Text: func(a map[string]string, today string, _ Level) (string, error) {
			d, err := day(a, today)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(`Explain %[1]s for me from goarmin.

1. get_calendar from=%[1]s to=%[1]s and get_daily from=%[1]s to=%[1]s for the day's summary.
2. get_series metric=heart_rate from_day=%[1]s max_points=300, and the same for stress and body_battery.
3. list_activities from_day=%[1]s, and list_nights from=%[1]s to=%[1]s for the night before.

Walk me through the day: the night, how energy and stress went, activities, and anything unusual. Keep it short.`, d), nil
		}},
}

func (p *Prompt) prompt() *mcp.Prompt {
	return &mcp.Prompt{Name: p.Name, Title: p.Title, Description: p.Desc, Arguments: p.Args}
}

func (s *Server) promptHandler(p *Prompt, l Level) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		start := s.now()
		c, ok := callerOf(req.Extra.TokenInfo)
		if !ok {
			return nil, errors.New("no token")
		}
		today := s.today(ctx, c)
		text, err := p.Text(req.Params.Arguments, today, min(l, c.level))
		errText := ""
		if err != nil {
			errText = err.Error()
		}
		s.logCall(c, "prompt:"+p.Name, start, errText)
		if err != nil {
			return nil, err
		}
		return &mcp.GetPromptResult{Description: p.Desc, Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: text}},
		}}, nil
	}
}

// today is the caller's local date: the account's time zone, else the watch's (its last known
// UTC offset), else UTC.
func (s *Server) today(ctx context.Context, c caller) string {
	now := s.now()
	if c.user.Timezone != "" {
		if loc, err := time.LoadLocation(c.user.Timezone); err == nil {
			return now.In(loc).Format(time.DateOnly)
		}
	}
	if z, err := s.st.Zone(ctx, c.user.ID); err == nil {
		return z.Day(now.Unix())
	}
	return now.UTC().Format(time.DateOnly)
}
