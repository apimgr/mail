package graphql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/mailservice"
	"golang.org/x/net/idna"
)

type paginationArgs struct {
	Page  *int32
	Limit *int32
}

type pagination struct {
	Page  int32
	Limit int32
	Total int32
	Pages int32
}

type userPage struct {
	Data       []mailUser
	Pagination pagination
}

type domainPage struct {
	Data       []mailDomain
	Pagination pagination
}

type aliasPage struct {
	Data       []mailAlias
	Pagination pagination
}

type forwardPage struct {
	Data       []mailForward
	Pagination pagination
}

type queuePage struct {
	Data       []queueEntry
	Pagination pagination
}

type queueStats struct {
	CurrentQueueSize int32
	TotalSizeBytes   Long
	ByState          []queueState
}

type (
	userArgs   struct{ ID string }
	domainArgs struct{ Domain string }
)

type dnsRecord struct {
	Type       string
	Name       string
	Value      string
	Preference *int32
}

type dnsView struct {
	Records []dnsRecord
	Missing []string
}

type dkimView struct {
	Selector string
	Record   string
}

type healthSummary struct {
	Installed int32
	Running   int32
	Stopped   int32
	Unknown   int32
}

type healthView struct {
	Status   string
	Summary  healthSummary
	Services []mailService
}

type queueState struct {
	State string
	Count int32
}

type mailUser struct {
	ID         string
	Kind       string
	Email      string
	LocalPart  string
	Domain     string
	QuotaBytes Long
	Enabled    bool
	CreatedAt  Long
	UpdatedAt  Long
}

type mailDomain struct {
	ID           string
	Domain       string
	Description  string
	MaxMailboxes Long
	MaxAliases   Long
	MaxQuotaMB   Long
	Enabled      bool
	CreatedAt    Long
	UpdatedAt    Long
}

type mailAlias struct {
	ID          string
	Source      string
	Destination string
	Domain      string
	Enabled     bool
	CreatedAt   Long
}

type mailForward struct {
	ID           string
	Source       string
	Destinations []string
	KeepLocal    bool
	Enabled      bool
	CreatedAt    Long
}

type queueEntry struct {
	ID          string
	Queue       string
	Size        Long
	Sender      string
	Recipients  []string
	ArrivalTime string
	Reason      string
}

type mailService struct {
	Name           string
	Installed      bool
	Status         string
	Running        *bool
	Enabled        *bool
	PID            int32
	Uptime         string
	Version        string
	Executable     string
	Packages       []string
	ServiceAccount string
}

type (
	entryArgs   struct{ ID string }
	serviceArgs struct{ Name string }
)

type queryResolver struct {
	db    *database.DB
	queue mailservice.QueueManager
}

func (q *queryResolver) MailUser(ctx context.Context, args userArgs) (*mailUser, error) {
	if q.db == nil || q.db.UsersReadDB == nil {
		return nil, fmt.Errorf("mail user storage is unavailable")
	}
	item, err := q.db.GetMailUser(ctx, args.ID)
	if err != nil {
		return nil, err
	}
	return &mailUser{ID: item.ID, Kind: "virtual", Email: item.Email, LocalPart: item.LocalPart, Domain: item.Domain, QuotaBytes: Long(item.QuotaBytes), Enabled: item.Enabled, CreatedAt: Long(item.CreatedAt), UpdatedAt: Long(item.UpdatedAt)}, nil
}

func (q *queryResolver) MailDomain(ctx context.Context, args domainArgs) (*mailDomain, error) {
	if q.db == nil || q.db.UsersReadDB == nil {
		return nil, fmt.Errorf("mail domain storage is unavailable")
	}
	domain, err := normalizeGraphDomain(args.Domain)
	if err != nil {
		return nil, err
	}
	item, err := q.db.GetMailDomain(ctx, domain)
	if err != nil {
		return nil, err
	}
	return graphDomain(item), nil
}

func (q *queryResolver) MailDNS(ctx context.Context, args domainArgs) (*dnsView, error) {
	if q.db == nil || q.db.UsersReadDB == nil {
		return nil, fmt.Errorf("mail DNS storage is unavailable")
	}
	domain, err := normalizeGraphDomain(args.Domain)
	if err != nil {
		return nil, err
	}
	records, missing, err := q.db.GetMailDNSRecords(ctx, domain)
	if err != nil {
		return nil, err
	}
	view := &dnsView{Records: make([]dnsRecord, 0, len(records)), Missing: missing}
	for _, record := range records {
		var preference *int32
		if record.Preference != nil {
			value := int32(*record.Preference)
			preference = &value
		}
		view.Records = append(view.Records, dnsRecord{Type: record.Type, Name: record.Name, Value: record.Value, Preference: preference})
	}
	return view, nil
}

func (q *queryResolver) MailDKIM(ctx context.Context, args domainArgs) ([]dkimView, error) {
	if q.db == nil || q.db.UsersReadDB == nil {
		return nil, fmt.Errorf("mail DKIM storage is unavailable")
	}
	domain, err := normalizeGraphDomain(args.Domain)
	if err != nil {
		return nil, err
	}
	keys, err := q.db.ListMailDKIMKeys(ctx, domain)
	if err != nil {
		return nil, err
	}
	result := make([]dkimView, 0, len(keys))
	for _, key := range keys {
		result = append(result, dkimView{Selector: key.Selector, Record: "v=DKIM1; k=rsa; p=" + key.PublicKey})
	}
	return result, nil
}

func (*queryResolver) MailHealth() *healthView {
	components := mailservice.Inspect()
	view := &healthView{Services: graphServices(components)}
	for _, item := range components {
		if !item.Installed {
			continue
		}
		view.Summary.Installed++
		if item.Running == nil {
			view.Summary.Unknown++
		} else if *item.Running {
			view.Summary.Running++
		} else {
			view.Summary.Stopped++
		}
	}
	switch {
	case view.Summary.Installed == 0:
		view.Status = "unavailable"
	case view.Summary.Stopped > 0:
		view.Status = "degraded"
	case view.Summary.Unknown > 0:
		view.Status = "unknown"
	default:
		view.Status = "healthy"
	}
	return view
}

func normalizeGraphDomain(value string) (string, error) {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if value == "" || strings.ContainsAny(value, "/:@\\") {
		return "", fmt.Errorf("invalid mail domain")
	}
	domain, err := idna.Lookup.ToASCII(value)
	if err != nil || len(domain) > 253 {
		return "", fmt.Errorf("invalid mail domain")
	}
	return domain, nil
}

func (q *queryResolver) MailUsers(ctx context.Context, args paginationArgs) (*userPage, error) {
	page, limit, err := normalizedPagination(args)
	if err != nil {
		return nil, err
	}
	if q.db == nil || q.db.UsersReadDB == nil {
		return nil, fmt.Errorf("mail user storage is unavailable")
	}
	items, total, err := q.db.ListMailUsers(ctx, page, limit)
	data := make([]mailUser, 0, len(items))
	for _, item := range items {
		data = append(data, mailUser{ID: item.ID, Email: item.Email, LocalPart: item.LocalPart, Domain: item.Domain, QuotaBytes: Long(item.QuotaBytes), Enabled: item.Enabled, CreatedAt: Long(item.CreatedAt), UpdatedAt: Long(item.UpdatedAt)})
	}
	return &userPage{Data: data, Pagination: makePagination(page, limit, total)}, err
}

func (q *queryResolver) MailDomains(ctx context.Context, args paginationArgs) (*domainPage, error) {
	page, limit, err := normalizedPagination(args)
	if err != nil {
		return nil, err
	}
	if q.db == nil || q.db.UsersReadDB == nil {
		return nil, fmt.Errorf("mail domain storage is unavailable")
	}
	items, total, err := q.db.ListMailDomains(ctx, page, limit)
	data := make([]mailDomain, 0, len(items))
	for _, item := range items {
		data = append(data, *graphDomain(item))
	}
	return &domainPage{Data: data, Pagination: makePagination(page, limit, total)}, err
}

func (q *queryResolver) MailAliases(ctx context.Context, args paginationArgs) (*aliasPage, error) {
	page, limit, err := normalizedPagination(args)
	if err != nil {
		return nil, err
	}
	if q.db == nil || q.db.UsersReadDB == nil {
		return nil, fmt.Errorf("mail alias storage is unavailable")
	}
	items, total, err := q.db.ListMailAliases(ctx, page, limit)
	data := make([]mailAlias, 0, len(items))
	for _, item := range items {
		data = append(data, mailAlias{ID: item.ID, Source: item.Source, Destination: item.Destination, Domain: item.Domain, Enabled: item.Enabled, CreatedAt: Long(item.CreatedAt)})
	}
	return &aliasPage{Data: data, Pagination: makePagination(page, limit, total)}, err
}

func (q *queryResolver) MailForwards(ctx context.Context, args paginationArgs) (*forwardPage, error) {
	page, limit, err := normalizedPagination(args)
	if err != nil {
		return nil, err
	}
	if q.db == nil || q.db.UsersReadDB == nil {
		return nil, fmt.Errorf("mail forward storage is unavailable")
	}
	items, total, err := q.db.ListMailForwards(ctx, page, limit)
	data := make([]mailForward, 0, len(items))
	for _, item := range items {
		data = append(data, mailForward{ID: item.ID, Source: item.Source, Destinations: item.Destinations, KeepLocal: item.KeepLocal, Enabled: item.Enabled, CreatedAt: Long(item.CreatedAt)})
	}
	return &forwardPage{Data: data, Pagination: makePagination(page, limit, total)}, err
}

func (q *queryResolver) MailQueue(ctx context.Context, args paginationArgs) (*queuePage, error) {
	page, limit, err := normalizedPagination(args)
	if err != nil {
		return nil, err
	}
	items, err := q.queue.List(ctx)
	if err != nil {
		return nil, err
	}
	total := len(items)
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	data := make([]queueEntry, 0, end-start)
	for _, item := range items[start:end] {
		data = append(data, queueEntryFrom(item))
	}
	pages := 0
	if total > 0 {
		pages = (total-1)/limit + 1
	}
	return &queuePage{Data: data, Pagination: pagination{Page: int32(page), Limit: int32(limit), Total: int32(total), Pages: int32(pages)}}, nil
}

func (q *queryResolver) MailQueueEntry(ctx context.Context, args entryArgs) (queueEntry, error) {
	item, err := q.queue.Get(ctx, args.ID)
	return queueEntryFrom(item), err
}

func (q *queryResolver) MailQueueStats(ctx context.Context) (*queueStats, error) {
	entries, err := q.queue.List(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int32)
	var size int64
	for _, item := range entries {
		counts[item.Queue]++
		size += item.Size
	}
	states := make([]queueState, 0, len(counts))
	for name, count := range counts {
		states = append(states, queueState{State: name, Count: count})
	}
	return &queueStats{CurrentQueueSize: int32(len(entries)), TotalSizeBytes: Long(size), ByState: states}, nil
}

func (*queryResolver) MailServices() []mailService { return graphServices(mailservice.Inspect()) }

func (*queryResolver) MailService(args serviceArgs) (*mailService, error) {
	component, err := mailservice.InspectOne(args.Name)
	if err != nil {
		return nil, nil
	}
	services := graphServices([]mailservice.Component{component})
	return &services[0], nil
}

func queueEntryFrom(item mailservice.QueueEntry) queueEntry {
	return queueEntry{ID: item.ID, Queue: item.Queue, Size: Long(item.Size), Sender: item.Sender, Recipients: item.Recipients, ArrivalTime: item.ArrivalTime.UTC().Format(time.RFC3339), Reason: item.Reason}
}

func graphServices(services []mailservice.Component) []mailService {
	result := make([]mailService, 0, len(services))
	for _, item := range services {
		packages := append([]string{}, item.Packages...)
		result = append(result, mailService{Name: item.Name, Installed: item.Installed, Status: item.Status, Running: item.Running, Enabled: item.Enabled, PID: int32(item.PID), Uptime: item.Uptime, Version: item.Version, Executable: item.Executable, Packages: packages, ServiceAccount: item.ServiceAccount})
	}
	return result
}

func graphDomain(item database.MailDomain) *mailDomain {
	return &mailDomain{ID: item.ID, Domain: item.Domain, Description: item.Description, MaxMailboxes: Long(item.MaxMailboxes), MaxAliases: Long(item.MaxAliases), MaxQuotaMB: Long(item.MaxQuotaMB), Enabled: item.Enabled, CreatedAt: Long(item.CreatedAt), UpdatedAt: Long(item.UpdatedAt)}
}

func normalizedPagination(args paginationArgs) (int, int, error) {
	page, limit := 1, 250
	if args.Page != nil {
		page = int(*args.Page)
	}
	if args.Limit != nil {
		limit = int(*args.Limit)
	}
	if page < 1 || limit < 1 || limit > 250 || page > int(^uint(0)>>1)/limit {
		return 0, 0, fmt.Errorf("pagination is outside the allowed range")
	}
	return page, limit, nil
}

func makePagination(page, limit int, total int64) pagination {
	pages := int64(0)
	if total > 0 {
		pages = (total-1)/int64(limit) + 1
	}
	return pagination{Page: int32(page), Limit: int32(limit), Total: int32(total), Pages: int32(pages)}
}
