package demo

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/jira"
)

// The Acme Shop team: a web store in the middle of a checkout rewrite, a
// search project and a round of mobile performance work. Times are relative
// to when the demo starts.

var (
	me   = jira.UserField{AccountID: "acc-sam", DisplayName: "Sam Rivera"}
	ana  = jira.UserField{AccountID: "acc-ana", DisplayName: "Ana Díaz"}
	leo  = jira.UserField{AccountID: "acc-leo", DisplayName: "Leo Martín"}
	maya = jira.UserField{AccountID: "acc-maya", DisplayName: "Maya Chen"}
	tom  = jira.UserField{AccountID: "acc-tom", DisplayName: "Tom Becker"}

	people = []jira.UserField{me, ana, leo, maya, tom}
)

func status(id, name, cat string) jira.StatusField {
	names := map[string]string{"new": "To Do", "indeterminate": "In Progress", "done": "Done"}
	ids := map[string]int{"new": 2, "indeterminate": 4, "done": 3}
	return jira.StatusField{ID: id, Name: name, StatusCategory: &jira.StatusCategory{ID: ids[cat], Key: cat, Name: names[cat]}}
}

var (
	stBacklog  = status("1", "Backlog", "new")
	stRefine   = status("2", "In Refinement", "new")
	stProgress = status("3", "In Progress", "indeterminate")
	stReview   = status("4", "In Review", "indeterminate")
	stDone     = status("5", "Done", "done")

	statuses = []jira.StatusField{stBacklog, stRefine, stProgress, stReview, stDone}

	tEpic  = jira.IssueTypeField{ID: "10000", Name: "Epic", HierarchyLevel: 1}
	tStory = jira.IssueTypeField{ID: "10001", Name: "Story"}
	tTask  = jira.IssueTypeField{ID: "10002", Name: "Task"}
	tBug   = jira.IssueTypeField{ID: "10003", Name: "Bug"}
	tSpike = jira.IssueTypeField{ID: "10004", Name: "Spike"}
	tSub   = jira.IssueTypeField{ID: "10005", Name: "Sub-task", Subtask: true, HierarchyLevel: -1}

	issueTypes = []jira.IssueTypeField{tEpic, tStory, tTask, tBug, tSpike, tSub}
)

// ─── Atlassian Document Format ───────────────────────────────────

type node = map[string]any

func text(s string, marks ...string) node {
	n := node{"type": "text", "text": s}
	if len(marks) > 0 {
		var ms []node
		for _, m := range marks {
			ms = append(ms, node{"type": m})
		}
		n["marks"] = ms
	}
	return n
}

func link(s, href string) node {
	return node{"type": "text", "text": s, "marks": []node{{"type": "link", "attrs": node{"href": href}}}}
}

func para(parts ...node) node { return node{"type": "paragraph", "content": parts} }
func p(s string) node         { return para(text(s)) }
func h(level int, s string) node {
	return node{"type": "heading", "attrs": node{"level": level}, "content": []node{text(s)}}
}

func list(items ...node) node {
	var li []node
	for _, it := range items {
		li = append(li, node{"type": "listItem", "content": []node{it}})
	}
	return node{"type": "bulletList", "content": li}
}

func steps(items ...string) node {
	var li []node
	for _, it := range items {
		li = append(li, node{"type": "listItem", "content": []node{p(it)}})
	}
	return node{"type": "orderedList", "content": li}
}

func code(lang, s string) node {
	return node{"type": "codeBlock", "attrs": node{"language": lang}, "content": []node{text(s)}}
}

func doc(content ...node) json.RawMessage {
	b, _ := json.Marshal(node{"type": "doc", "version": 1, "content": content})
	return b
}

// ─── the issues ──────────────────────────────────────────────────

type seed struct {
	n        int
	typ      jira.IssueTypeField
	st       jira.StatusField
	summary  string
	assignee *jira.UserField
	reporter jira.UserField
	parent   int
	prio     string
	labels   []string
	updated  time.Duration // before start
	due      int           // days from start; 0 = none
	desc     json.RawMessage
}

func (s *site) seed() {
	u := func(x jira.UserField) *jira.UserField { return &x }
	seeds := []seed{
		{101, tEpic, stProgress, "Checkout v2: a one-page checkout", u(me), maya, 0, "High", []string{"checkout"}, 3 * time.Hour, 24,
			doc(h(2, "Why"), p("Half of the carts that reach checkout are abandoned on the shipping step. A single page with inline validation should cut that in half."),
				h(2, "Scope"), list(p("One page: address, shipping, payment, review"), p("Guest checkout keeps the cart across devices"), p("Behind the checkout-v2 flag until conversion holds for two weeks")),
				h(2, "Out of scope"), p("New payment methods (see SHOP-104)."))},
		{102, tEpic, stProgress, "Search relevance for the autumn catalog", u(ana), maya, 0, "Medium", []string{"search"}, 26 * time.Hour, 0,
			doc(p("Autumn brings 4,000 new products. Make sure people find what's in stock, with the words they actually use."))},
		{103, tEpic, stRefine, "Faster product pages on mobile", u(leo), leo, 0, "Medium", []string{"performance"}, 50 * time.Hour, 0,
			doc(p("Largest Contentful Paint is 3.4 s at p75 on mobile. Goal: under 2.5 s."))},
		{104, tEpic, stBacklog, "Apple Pay and Google Pay", u(maya), maya, 0, "Medium", []string{"payments"}, 9 * 24 * time.Hour, 0,
			doc(p("Wallet payments, starting with Apple Pay on Safari."))},

		{110, tStory, stReview, "One-page checkout layout", u(me), maya, 101, "High", []string{"frontend", "checkout"}, 2 * time.Hour, 5,
			doc(h(3, "Acceptance criteria"), list(p("Address, shipping and payment on one page, in that order"), p("Errors show next to the field, not in a banner"), p("Works at 360 px wide")),
				para(text("Designs: "), link("Figma, checkout v2", "https://www.figma.com/file/checkout-v2")))},
		{111, tTask, stProgress, "Address autocomplete with postal code lookup", u(me), me, 101, "Medium", []string{"frontend"}, 40 * time.Minute, 0,
			doc(p("Typing a postal code fills in city and region; the street suggests from the same lookup."),
				code("ts", "const place = await geo.lookup({ postalCode, country })\nform.patch({ city: place.city, region: place.region })"))},
		{112, tBug, stProgress, "Coupon disappears when the shipping method changes", u(leo), ana, 101, "High", []string{"checkout", "bug-bash"}, 5 * time.Hour, 0,
			doc(h(3, "Steps"), steps("Add any product and go to checkout", "Apply the coupon AUTUMN10", "Switch shipping from Standard to Express"),
				p("Expected: the discount stays. Actual: the coupon is gone and the total goes back up."))},
		{113, tTask, stBacklog, "Keep guest carts across devices", nil, maya, 101, "Medium", nil, 6 * 24 * time.Hour, 0,
			doc(p("Link the cart to the email as soon as the guest types it."))},
		{114, tSub, stProgress, "Update the checkout end-to-end tests", u(me), me, 110, "Medium", []string{"testing"}, 90 * time.Minute, 0, nil},
		{115, tSub, stBacklog, "Accessibility pass on the new form", u(me), me, 110, "Medium", []string{"a11y"}, 26 * time.Hour, 0, nil},
		{116, tTask, stDone, "Feature flag and rollout plan for checkout v2", u(me), maya, 101, "Medium", nil, 4 * 24 * time.Hour, 0,
			doc(list(p("Week 1: staff only"), p("Week 2: 10% of traffic"), p("Week 3: 50%, then everyone if conversion holds")))},

		{120, tStory, stProgress, "Boost in-stock products in results", u(ana), maya, 102, "High", []string{"search"}, 7 * time.Hour, 0,
			doc(p("Out-of-stock products drop below everything in stock, unless the query is an exact product name."))},
		{121, tTask, stRefine, "Synonyms for seasonal words (jumper, sweater, pullover)", u(ana), ana, 102, "Medium", []string{"search"}, 30 * time.Hour, 0, nil},
		{122, tBug, stBacklog, "Search suggestions show sold-out products", nil, tom, 102, "High", []string{"search"}, 3 * 24 * time.Hour, 0,
			doc(p("Suggestions come from the old index, which has no stock field."))},

		{130, tTask, stRefine, "Serve product images as AVIF", u(leo), leo, 103, "Medium", []string{"performance"}, 2 * 24 * time.Hour, 0, nil},
		{131, tTask, stBacklog, "Lazy-load reviews below the fold", u(me), leo, 103, "Low", []string{"performance"}, 8 * 24 * time.Hour, 0, nil},
		{132, tSpike, stDone, "Measure LCP on low-end Android phones", u(leo), leo, 103, "Medium", nil, 5 * 24 * time.Hour, 0,
			doc(p("p75 LCP: 3.4 s on a Moto G, 2.1 s on a Pixel 8. The hero image is most of it."))},

		{140, tTask, stBacklog, "Apple Pay merchant validation endpoint", u(maya), maya, 104, "Medium", []string{"payments"}, 9 * 24 * time.Hour, 0, nil},

		{150, tBug, stProgress, "Order confirmation email shows the time in UTC", u(me), tom, 0, "Highest", []string{"email"}, 25 * time.Minute, 2,
			doc(p("Customers in Madrid see their order placed at 07:42 when it was 09:42."),
				code("go", "// fix: format in the customer's time zone\nplaced := order.PlacedAt.In(customer.Location())"))},
		{151, tTask, stRefine, "Rotate the payment provider API keys", u(me), maya, 0, "High", []string{"security"}, 20 * time.Hour, 9, nil},
		{152, tBug, stBacklog, "Safari: cart badge doesn't update after adding an item", u(tom), ana, 0, "Medium", []string{"frontend"}, 4 * 24 * time.Hour, 0, nil},
		{153, tTask, stDone, "Upgrade the storefront to Node 22", u(me), me, 0, "Medium", []string{"maintenance"}, 3 * 24 * time.Hour, 0, nil},
		{154, tTask, stDone, "Weekly dependency updates", u(tom), tom, 0, "Low", []string{"maintenance"}, 10 * 24 * time.Hour, 0, nil},
		{155, tStory, stBacklog, "Wishlists: share a list with a link", u(maya), maya, 0, "Medium", nil, 12 * 24 * time.Hour, 0, nil},
		{156, tBug, stReview, "Double charge when retrying a failed payment", u(maya), tom, 0, "Highest", []string{"payments", "incident"}, 4 * time.Hour, 0,
			doc(p("When the provider times out and the customer presses Pay again, both attempts can succeed."),
				h(3, "Fix"), p("Send an idempotency key per order attempt, and treat a duplicate as success."))},
		{157, tTask, stProgress, "Document the checkout architecture", u(leo), leo, 0, "Low", []string{"docs"}, 28 * time.Hour, 0, nil},
	}
	for _, sd := range seeds {
		s.add(sd)
	}

	s.comments["SHOP-110"] = []jira.Comment{
		s.comment(maya, 50*time.Hour, "Design review went well. One change: the order summary stays visible on the right on desktop."),
		s.comment(me, 3*time.Hour, "Done, and the PR is up: summary is sticky on desktop and collapses above the form on mobile."),
		s.comment(leo, 2*time.Hour, "Reviewed. Two nits on the form labels, otherwise good to go."),
	}
	s.comments["SHOP-112"] = []jira.Comment{
		s.comment(ana, 30*time.Hour, "Happens only with coupons that have a minimum order value."),
		s.comment(leo, 5*time.Hour, "Found it: the shipping change recalculates the cart without the coupon. Fix on the way."),
	}
	s.comments["SHOP-150"] = []jira.Comment{
		s.comment(tom, 26*time.Hour, "Three customers wrote to support about it today."),
	}
	s.comments["SHOP-156"] = []jira.Comment{
		s.comment(tom, 20*time.Hour, "Refunded the four customers affected this week."),
		s.comment(maya, 4*time.Hour, "Idempotency keys are in, ready for review."),
	}
	s.watching = map[string]bool{"SHOP-156": true, "SHOP-120": true, "SHOP-157": true, "SHOP-112": true}

	s.link("SHOP-156", "SHOP-150", "Relates", "relates to", "relates to")
	s.link("SHOP-112", "SHOP-110", "Blocks", "is blocked by", "blocks")
	s.issues["SHOP-112"].Fields.Attachments = []jira.Attachment{{ID: "att-1", Filename: "coupon-gone.png", Size: 184320, MimeType: "image/png"}}
}

func (s *site) add(sd seed) {
	key := fmt.Sprintf("SHOP-%d", sd.n)
	is := &jira.Issue{ID: fmt.Sprint(20000 + sd.n), Key: key}
	f := &is.Fields
	f.Summary, f.Status, f.IssueType, f.Assignee = sd.summary, sd.st, sd.typ, sd.assignee
	rep := sd.reporter
	f.Reporter = &rep
	f.Priority = &jira.NameField{Name: sd.prio}
	f.Labels = sd.labels
	f.Created = jtime(s.start.Add(-sd.updated - time.Duration(3+sd.n%9)*24*time.Hour))
	f.Updated = jtime(s.start.Add(-sd.updated))
	if sd.due > 0 {
		f.DueDate = s.start.AddDate(0, 0, sd.due).Format("2006-01-02")
	}
	if sd.st.CategoryKey() == "done" {
		f.Resolution = &jira.NameField{Name: "Done"}
	}
	f.Description = sd.desc
	if f.Description == nil {
		f.Description = doc(p(sd.summary + "."))
	}
	if sd.parent != 0 {
		pk := fmt.Sprintf("SHOP-%d", sd.parent)
		parent := s.issues[pk]
		f.Parent = &jira.ParentField{Key: pk}
		f.Parent.Fields.Summary = parent.Fields.Summary
		f.Parent.Fields.Status = parent.Fields.Status
		f.Parent.Fields.IssueType = parent.Fields.IssueType
		f.Parent.Fields.Priority = parent.Fields.Priority
		if sd.typ.Subtask {
			rel := jira.RelatedIssue{Key: key}
			rel.Fields.Summary, rel.Fields.Status, rel.Fields.IssueType, rel.Fields.Priority = f.Summary, f.Status, f.IssueType, f.Priority
			parent.Fields.Subtasks = append(parent.Fields.Subtasks, rel)
		}
	}
	s.issues[key] = is
	s.order = append(s.order, key)
	// A little history: how it got to its status.
	var items []jira.Changelog
	path := map[string][]string{
		"Backlog": nil, "In Refinement": {"Backlog"}, "In Progress": {"Backlog", "In Refinement"},
		"In Review": {"Backlog", "In Progress"}, "Done": {"Backlog", "In Progress", "In Review"},
	}[sd.st.Name]
	from := ""
	for i, to := range append(path, sd.st.Name) {
		if from != "" {
			who := me
			if sd.assignee != nil {
				who = *sd.assignee
			}
			at := s.start.Add(-sd.updated - time.Duration(len(path)-i)*7*time.Hour)
			items = append(items, jira.Changelog{ID: fmt.Sprintf("%s-h%d", key, i), Author: who, Created: jtime(at),
				Items: []jira.ChangelogItem{{Field: "status", FromString: from, ToString: to}}})
		}
		from = to
	}
	s.history[key] = items
}

func (s *site) comment(who jira.UserField, ago time.Duration, body string) jira.Comment {
	s.next++
	return jira.Comment{ID: fmt.Sprint("c", s.next), Author: who, Body: doc(p(body)), Created: jtime(s.start.Add(-ago))}
}

func (s *site) link(from, to, name, inward, outward string) {
	rel := func(k string) *jira.RelatedIssue {
		is := s.issues[k]
		r := &jira.RelatedIssue{Key: k}
		r.Fields.Summary, r.Fields.Status, r.Fields.IssueType, r.Fields.Priority = is.Fields.Summary, is.Fields.Status, is.Fields.IssueType, is.Fields.Priority
		return r
	}
	t := jira.IssueLinkType{ID: name, Name: name, Inward: inward, Outward: outward}
	s.issues[from].Fields.IssueLinks = append(s.issues[from].Fields.IssueLinks, jira.IssueLink{ID: from + to, Type: t, InwardIssue: rel(to)})
	s.issues[to].Fields.IssueLinks = append(s.issues[to].Fields.IssueLinks, jira.IssueLink{ID: to + from, Type: t, OutwardIssue: rel(from)})
}

func jtime(t time.Time) string { return t.Format("2006-01-02T15:04:05.000-0700") }
