package api

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jolyonbrown/point.vote/internal/room"
)

// wantsPlainText reports whether the caller asked for the terminal-dweller
// rendering. Browsers never put text/plain in their default Accept, so the
// web UI is unaffected.
func wantsPlainText(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/plain")
}

// renderText draws the room state as an aligned plain-text table. It
// renders from the same redacted State as JSON, so blindness holds by
// construction.
func renderText(st room.State) string {
	var b strings.Builder
	fmt.Fprintf(&b, "point.vote · %s\n", st.RoomID)
	subject := st.Round.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	fmt.Fprintf(&b, "round %d · %s — %s\n\n", st.Round.Seq, st.Round.State, subject)
	if st.Round.Context != "" {
		fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(st.Round.Context))
	}
	fmt.Fprintf(&b, "deck: %s\n\n", strings.Join(st.Deck, " "))

	if len(st.Round.Participants) == 0 {
		b.WriteString("nobody here yet. democracy awaits.\n")
	} else {
		nameW, kindW := utf8.RuneCountInString("who"), utf8.RuneCountInString("kind")
		for _, p := range st.Round.Participants {
			nameW = max(nameW, utf8.RuneCountInString(p.Name))
			kindW = max(kindW, utf8.RuneCountInString(p.Kind))
		}
		row := func(name, kind, voted string) {
			fmt.Fprintf(&b, "%-*s  %-*s  %s\n", nameW, name, kindW, kind, voted)
		}
		row("who", "kind", "voted")
		row(strings.Repeat("-", nameW), strings.Repeat("-", kindW), "-----")
		voters, waiting := 0, 0
		for _, p := range st.Round.Participants {
			tick := "."
			switch {
			case p.Kind == room.KindObserver:
				tick = "(observer)"
			case p.HasVoted:
				tick = "voted"
			default:
				waiting++
			}
			if p.Kind != room.KindObserver {
				voters++
			}
			row(p.Name, p.Kind, tick)
		}
		b.WriteString("\n")
		if st.Round.State == room.StateVoting {
			switch {
			case voters == 0:
				b.WriteString("no voters yet.\n")
			case waiting == 0:
				b.WriteString("all votes in.\n")
			default:
				fmt.Fprintf(&b, "waiting on %d of %d.\n", waiting, voters)
			}
		}
	}

	if st.Results != nil {
		b.WriteString("\nresults\n")
		nameW := 0
		valueW := utf8.RuneCountInString("value")
		for _, v := range st.Results.Votes {
			nameW = max(nameW, utf8.RuneCountInString(v.Name))
			valueW = max(valueW, utf8.RuneCountInString(v.Value))
		}
		for _, v := range st.Results.Votes {
			fmt.Fprintf(&b, "%-*s  %-*s", nameW, v.Name, valueW, v.Value)
			if v.Rationale != "" {
				fmt.Fprintf(&b, "  %q", v.Rationale)
			}
			b.WriteString("\n")
		}
		if len(st.Results.Votes) == 0 {
			b.WriteString("nobody voted. a bold collective statement.\n")
		}
		b.WriteString("\n" + statsLine(st.Results.Stats) + "\n")
	}

	if len(st.History) > 0 {
		b.WriteString("\nhistory\n")
		for i := len(st.History) - 1; i >= 0; i-- {
			h := st.History[i]
			subject := h.Subject
			if subject == "" {
				subject = "(untitled)"
			}
			fmt.Fprintf(&b, "#%d %s · %s\n", h.Seq, subject, statsLine(h.Stats))
		}
	}
	return b.String()
}

func statsLine(s room.Stats) string {
	numeric := func(f *float64) string {
		if f == nil {
			return "-"
		}
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", *f), "0"), ".")
	}
	return fmt.Sprintf("spread %s · median %s · mean %s · consensus %v",
		numeric(s.Spread), numeric(s.Median), numeric(s.Mean), s.Consensus)
}
