// Package ui implements the server-side-rendered HTML frontend for the Shop service.
package ui

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shophub-project-2026/shop/internal/articles"
	"github.com/shophub-project-2026/shop/internal/cart"
	"github.com/shophub-project-2026/shop/internal/orders"
)

//go:embed templates
var templateFS embed.FS

var funcMap = template.FuncMap{
	"not": func(v any) bool {
		switch val := v.(type) {
		case bool:
			return !val
		case int:
			return val == 0
		case string:
			return val == ""
		case []articles.Article:
			return len(val) == 0
		case []orders.Order:
			return len(val) == 0
		case []cart.Item:
			return len(val) == 0
		default:
			return v == nil
		}
	},
	"slice": func(s string, i, j int) string {
		if j > len(s) {
			j = len(s)
		}
		return s[i:j]
	},
	"ne": func(a, b string) bool { return a != b },
}

func parse(name string) *template.Template {
	return template.Must(
		template.New("layout.html").
			Funcs(funcMap).
			ParseFS(templateFS,
				"templates/layout.html",
				"templates/"+name,
			),
	)
}

// Handler serves all HTML pages.
type Handler struct {
	articleRepo articles.Repository
	orderRepo   orders.Repository
	cartStore   *cart.Store
	adminKey    string
	ethWallet   string
	ethPrice    float64
	logger      *slog.Logger
}

// NewHandler constructs a UI Handler.
func NewHandler(
	articleRepo articles.Repository,
	orderRepo orders.Repository,
	cartStore *cart.Store,
	adminKey string,
	ethWallet string,
	ethPrice float64,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		articleRepo: articleRepo,
		orderRepo:   orderRepo,
		cartStore:   cartStore,
		adminKey:    adminKey,
		ethWallet:   ethWallet,
		ethPrice:    ethPrice,
		logger:      logger,
	}
}

// RegisterRoutes wires all HTML page routes onto mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// customer
	mux.HandleFunc("GET /{$}", h.articleList)
	mux.HandleFunc("GET /articles/{id}", h.articleDetail)
	mux.HandleFunc("POST /cart", h.cartAdd)
	mux.HandleFunc("POST /cart/remove", h.cartRemove)
	mux.HandleFunc("GET /cart", h.cartView)
	mux.HandleFunc("GET /checkout", h.checkout)

	// admin
	mux.HandleFunc("GET /admin/articles", h.adminArticles)
	mux.HandleFunc("GET /admin/articles/new", h.adminArticleNew)
	mux.HandleFunc("POST /admin/articles/new", h.adminArticleCreate)
	mux.HandleFunc("GET /admin/articles/{id}/edit", h.adminArticleEdit)
	mux.HandleFunc("POST /admin/articles/{id}/edit", h.adminArticleUpdate)
	mux.HandleFunc("POST /admin/articles/{id}/delete", h.adminArticleDelete)
	mux.HandleFunc("GET /admin/orders", h.adminOrders)
}

// ── customer handlers ──────────────────────────────────────────────────────

func (h *Handler) articleList(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	list, err := h.articleRepo.List(r.Context(), search)
	if err != nil {
		h.serverError(w, err)
		return
	}
	h.render(w, "customer/articles.html", map[string]any{
		"Articles": list,
		"Search":   search,
	})
}

func (h *Handler) articleDetail(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := h.articleRepo.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, "customer/article_detail.html", map[string]any{
		"Article": a,
		"Err":     r.URL.Query().Get("err"),
	})
}

func (h *Handler) cartAdd(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	articleID, err := uuid.Parse(r.FormValue("article_id"))
	if err != nil {
		http.Error(w, "bad article id", http.StatusBadRequest)
		return
	}
	qty, _ := strconv.Atoi(r.FormValue("quantity"))
	if qty <= 0 {
		qty = 1
	}
	wallet := strings.TrimSpace(r.FormValue("wallet_address"))
	if !isValidWallet(wallet) {
		http.Redirect(w, r, "/articles/"+articleID.String()+"?err=wallet_required", http.StatusSeeOther)
		return
	}
	h.cartStore.Add(wallet, articleID, qty)
	http.Redirect(w, r, "/cart?wallet="+wallet, http.StatusSeeOther)
}

func (h *Handler) cartRemove(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	articleID, err := uuid.Parse(r.FormValue("article_id"))
	if err != nil {
		http.Error(w, "bad article id", http.StatusBadRequest)
		return
	}
	wallet := strings.TrimSpace(r.FormValue("wallet_address"))
	if !isValidWallet(wallet) {
		http.Error(w, "wallet address required", http.StatusBadRequest)
		return
	}
	h.cartStore.Remove(wallet, articleID)
	http.Redirect(w, r, "/cart?wallet="+wallet, http.StatusSeeOther)
}

// isValidWallet returns true if s looks like an Ethereum address (0x + 40 hex chars).
// We accept the EVM format because that is what MetaMask hands the customer.
func isValidWallet(s string) bool {
	if len(s) != 42 || s[0] != '0' || (s[1] != 'x' && s[1] != 'X') {
		return false
	}
	for i := 2; i < 42; i++ {
		c := s[i]
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}

func (h *Handler) cartView(w http.ResponseWriter, r *http.Request) {
	wallet := r.URL.Query().Get("wallet")
	c := h.cartStore.Get(wallet)
	h.render(w, "customer/cart.html", map[string]any{
		"Items":  c.Items,
		"Wallet": wallet,
	})
}

func (h *Handler) checkout(w http.ResponseWriter, r *http.Request) {
	wallet := r.URL.Query().Get("wallet")

	data := map[string]any{
		"EthPriceUSD":   h.ethPrice,
		"RecipientAddr": h.ethWallet,
	}

	if o, err := h.orderRepo.FindPendingByWallet(r.Context(), wallet); err == nil {
		data["OrderID"] = o.ID.String()
		data["TotalUSD"] = o.TotalAmount
		data["EthAmount"] = o.TotalAmount / h.ethPrice
	} else if !errors.Is(err, orders.ErrNotFound) {
		h.serverError(w, err)
		return
	}

	// if no pending order, create one now from the cart
	if _, ok := data["OrderID"]; !ok {
		cartData := h.cartStore.Get(wallet)
		if len(cartData.Items) == 0 {
			http.Redirect(w, r, "/cart?wallet="+wallet, http.StatusSeeOther)
			return
		}
		input := orders.CreateInput{WalletAddress: wallet}
		for _, item := range cartData.Items {
			a, err := h.articleRepo.Get(r.Context(), item.ArticleID)
			if err != nil {
				continue
			}
			input.Items = append(input.Items, orders.ItemInput{
				ArticleID: item.ArticleID,
				Quantity:  item.Quantity,
				UnitPrice: a.Price,
			})
		}
		if len(input.Items) == 0 {
			http.Redirect(w, r, "/cart?wallet="+wallet, http.StatusSeeOther)
			return
		}
		o, err := h.orderRepo.Create(r.Context(), input)
		if err != nil {
			data["Error"] = fmt.Sprintf("Could not create order: %v", err)
		} else {
			h.cartStore.Clear(wallet)
			data["OrderID"] = o.ID.String()
			data["TotalUSD"] = o.TotalAmount
			data["EthAmount"] = o.TotalAmount / h.ethPrice
		}
	}

	h.render(w, "customer/checkout.html", data)
}

// ── admin handlers ─────────────────────────────────────────────────────────

func (h *Handler) adminArticles(w http.ResponseWriter, r *http.Request) {
	if !h.checkAdmin(w, r) {
		return
	}
	list, err := h.articleRepo.List(r.Context(), "")
	if err != nil {
		h.serverError(w, err)
		return
	}
	h.render(w, "admin/articles.html", map[string]any{
		"Articles": list,
		"Flash":    r.URL.Query().Get("flash"),
	})
}

func (h *Handler) adminArticleNew(w http.ResponseWriter, r *http.Request) {
	if !h.checkAdmin(w, r) {
		return
	}
	h.render(w, "admin/article_form.html", map[string]any{
		"IsEdit":  false,
		"Article": articles.Article{},
	})
}

func (h *Handler) adminArticleCreate(w http.ResponseWriter, r *http.Request) {
	if !h.checkAdmin(w, r) {
		return
	}
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	price, _ := strconv.ParseFloat(r.FormValue("price"), 64)
	qty, _ := strconv.Atoi(r.FormValue("quantity"))

	if name == "" || price <= 0 {
		h.render(w, "admin/article_form.html", map[string]any{
			"IsEdit":  false,
			"Article": articles.Article{Name: name, Price: price, Quantity: qty},
			"Error":   "Name and a positive price are required.",
		})
		return
	}
	if _, err := h.articleRepo.Create(r.Context(), articles.CreateInput{Name: name, Price: price, Quantity: qty}); err != nil {
		h.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/articles?flash=Article+created", http.StatusSeeOther)
}

func (h *Handler) adminArticleEdit(w http.ResponseWriter, r *http.Request) {
	if !h.checkAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := h.articleRepo.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, "admin/article_form.html", map[string]any{"IsEdit": true, "Article": a})
}

func (h *Handler) adminArticleUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.checkAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	price, _ := strconv.ParseFloat(r.FormValue("price"), 64)
	qty, _ := strconv.Atoi(r.FormValue("quantity"))

	in := articles.UpdateInput{Name: name, Price: &price, Quantity: &qty}
	if _, err := h.articleRepo.Update(r.Context(), id, in); err != nil {
		h.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/articles?flash=Article+updated", http.StatusSeeOther)
}

func (h *Handler) adminArticleDelete(w http.ResponseWriter, r *http.Request) {
	if !h.checkAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = h.articleRepo.Delete(r.Context(), id)
	http.Redirect(w, r, "/admin/articles?flash=Article+deleted", http.StatusSeeOther)
}

func (h *Handler) adminOrders(w http.ResponseWriter, r *http.Request) {
	if !h.checkAdmin(w, r) {
		return
	}
	const pageSize = 20
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	list, total, err := h.orderRepo.List(r.Context(), pageSize, offset)
	if err != nil {
		h.serverError(w, err)
		return
	}
	if list == nil {
		list = []orders.Order{}
	}
	h.render(w, "admin/orders.html", map[string]any{
		"Orders":     list,
		"Total":      total,
		"HasPrev":    offset > 0,
		"HasNext":    offset+pageSize < total,
		"PrevOffset": offset - pageSize,
		"NextOffset": offset + pageSize,
	})
}

// ── helpers ────────────────────────────────────────────────────────────────

func (h *Handler) checkAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h.adminKey == "" {
		return true
	}
	// Accept key from cookie (browser) or header (API)
	key := r.Header.Get("X-Admin-Key")
	if key == "" {
		if c, err := r.Cookie("admin_key"); err == nil {
			key = c.Value
		}
	}
	if key != h.adminKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	tmpl := parse(name)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		h.logger.Error("render template", "name", name, "err", err)
	}
}

func (h *Handler) serverError(w http.ResponseWriter, err error) {
	h.logger.Error("ui server error", "err", err)
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}
