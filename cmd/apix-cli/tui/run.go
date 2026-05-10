package tui

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"github.com/rivo/tview"
)

type trafficItem struct {
	id        string
	requestID string
	method    string
	url       string
	timestamp string
	headers   map[string]string
	body      string
}

// Run starts an interactive terminal UI for live traffic inspection.
func Run(ctx context.Context, client apix.EngineClient) error {
	stream, err := client.CaptureTraffic(ctx, &apix.CaptureRequest{})
	if err != nil {
		return err
	}

	items := make([]trafficItem, 0, 256)
	table := tview.NewTable().SetBorders(false).SetSelectable(true, false)
	table.SetFixed(1, 0)
	details := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	status := tview.NewTextView().SetDynamicColors(true)
	status.SetText("[green]q[-]=quit  [green]↑/↓[-]=select  [green]r[-]=replay  [green]b[-]=breakpoint  [green]/[-]=filter")

	filterInput := tview.NewInputField().
		SetLabel("URL filter: ").
		SetFieldWidth(0)

	root := tview.NewFlex().SetDirection(tview.FlexRow)
	main := tview.NewFlex().
		AddItem(table, 0, 2, true).
		AddItem(details, 0, 3, false)
	root.AddItem(main, 0, 1, true)
	root.AddItem(filterInput, 1, 0, false)
	root.AddItem(status, 1, 0, false)

	app := tview.NewApplication().SetRoot(root, true)
	filter := ""

	refresh := func() {
		table.Clear()
		table.SetCell(0, 0, tview.NewTableCell("ID").SetSelectable(false).SetAttributes(tcell.AttrBold))
		table.SetCell(0, 1, tview.NewTableCell("METHOD").SetSelectable(false).SetAttributes(tcell.AttrBold))
		table.SetCell(0, 2, tview.NewTableCell("URL").SetSelectable(false).SetAttributes(tcell.AttrBold))
		table.SetCell(0, 3, tview.NewTableCell("TIME").SetSelectable(false).SetAttributes(tcell.AttrBold))
		row := 1
		for _, item := range items {
			if filter != "" && !strings.Contains(strings.ToLower(item.url), strings.ToLower(filter)) {
				continue
			}
			table.SetCell(row, 0, tview.NewTableCell(shortID(item.id)))
			table.SetCell(row, 1, tview.NewTableCell(item.method))
			table.SetCell(row, 2, tview.NewTableCell(item.url))
			table.SetCell(row, 3, tview.NewTableCell(item.timestamp))
			row++
		}
		if row <= 1 {
			details.SetText("No matching traffic yet.")
			return
		}
		curRow, _ := table.GetSelection()
		if curRow < 1 || curRow >= row {
			table.Select(1, 0)
		}
		renderDetails(table, details, items, filter)
	}

	filterInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter || key == tcell.KeyEscape {
			filter = filterInput.GetText()
			app.SetFocus(table)
			refresh()
		}
	})

	table.SetSelectionChangedFunc(func(row, _ int) {
		renderDetails(table, details, items, filter)
	})

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 'q':
				app.Stop()
				return nil
			case '/':
				filterInput.SetText(filter)
				app.SetFocus(filterInput)
				return nil
			case 'r':
				item, ok := selectedItem(table, items, filter)
				if !ok {
					return nil
				}
				go func(id string) {
					rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					_, callErr := client.ReplayRequest(rctx, &apix.ReplaySpec{
						Source:          &apix.ReplaySpec_RequestId{RequestId: id},
						FollowRedirects: true,
					})
					app.QueueUpdateDraw(func() {
						if callErr != nil {
							status.SetText(fmt.Sprintf("[red]replay failed: %v", callErr))
							return
						}
						status.SetText(fmt.Sprintf("[green]replayed request %s", shortID(id)))
					})
				}(item.id)
				return nil
			case 'b':
				item, ok := selectedItem(table, items, filter)
				if !ok {
					return nil
				}
				go func(url string) {
					rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					pattern := "^" + regexp.QuoteMeta(url) + "$"
					_, callErr := client.SetBreakpoint(rctx, &apix.BreakpointRule{UrlPattern: pattern, Enabled: true, Label: "from-tui"})
					app.QueueUpdateDraw(func() {
						if callErr != nil {
							status.SetText(fmt.Sprintf("[red]breakpoint failed: %v", callErr))
							return
						}
						status.SetText("[green]breakpoint added for selected URL")
					})
				}(item.url)
				return nil
			}
		case tcell.KeyCtrlC:
			app.Stop()
			return nil
		}
		return event
	})

	go func() {
		for {
			msg, recvErr := stream.Recv()
			if recvErr != nil {
				app.QueueUpdateDraw(func() {
					status.SetText(fmt.Sprintf("[yellow]stream ended: %v", recvErr))
				})
				return
			}
			item := trafficItem{
				id:        msg.Id,
				requestID: requestID(msg.Id, msg.Headers),
				method:    msg.Method,
				url:       msg.Url,
				timestamp: fmt.Sprintf("%d", msg.Timestamp),
				headers:   msg.Headers,
				body:      string(msg.Body),
			}
			app.QueueUpdateDraw(func() {
				items = append(items, item)
				if len(items) > 500 {
					items = items[len(items)-500:]
				}
				refresh()
			})
		}
	}()

	refresh()
	if err := app.Run(); err != nil {
		return err
	}
	return nil
}

func selectedItem(table *tview.Table, items []trafficItem, filter string) (trafficItem, bool) {
	row, _ := table.GetSelection()
	if row < 1 {
		return trafficItem{}, false
	}
	index := 0
	for _, item := range items {
		if filter != "" && !strings.Contains(strings.ToLower(item.url), strings.ToLower(filter)) {
			continue
		}
		index++
		if index == row {
			return item, true
		}
	}
	return trafficItem{}, false
}

func renderDetails(table *tview.Table, details *tview.TextView, items []trafficItem, filter string) {
	item, ok := selectedItem(table, items, filter)
	if !ok {
		details.SetText("No request selected.")
		return
	}
	var headerKeys []string
	for k := range item.headers {
		headerKeys = append(headerKeys, k)
	}
	sort.Strings(headerKeys)
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "[yellow]ID:[-] %s\n", item.id)
	_, _ = fmt.Fprintf(&sb, "[yellow]Request ID:[-] %s\n", item.requestID)
	_, _ = fmt.Fprintf(&sb, "[yellow]Method:[-] %s\n", item.method)
	_, _ = fmt.Fprintf(&sb, "[yellow]URL:[-] %s\n", item.url)
	_, _ = fmt.Fprintf(&sb, "[yellow]Timestamp:[-] %s\n\n", item.timestamp)
	sb.WriteString("[yellow]Headers[-]\n")
	for _, k := range headerKeys {
		_, _ = fmt.Fprintf(&sb, "%s: %s\n", k, item.headers[k])
	}
	if item.body != "" {
		sb.WriteString("\n[yellow]Body[-]\n")
		sb.WriteString(item.body)
	}
	details.SetText(sb.String())
}

func requestID(fallback string, headers map[string]string) string {
	for key, value := range headers {
		if strings.EqualFold(key, "X-Request-ID") && value != "" {
			return value
		}
	}
	return fallback
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
