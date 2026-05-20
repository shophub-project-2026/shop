// Package ui implements the server-side-rendered HTML frontend for the Shop service.
package ui

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
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
	mux.HandleFunc("POST /checkout", h.checkoutCreate)

	// admin
	mux.HandleFunc("GET /admin/login", h.adminLoginPage)
	mux.HandleFunc("POST /admin/login", h.adminLoginSubmit)
	mux.HandleFunc("POST /admin/logout", h.adminLogout)
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
	h.renderWithRequest(w, r, "customer/articles.html", map[string]any{
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
	h.renderWithRequest(w, r, "customer/article_detail.html", map[string]any{
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
	h.renderWithRequest(w, r, "customer/cart.html", map[string]any{
		"Items":  c.Items,
		"Wallet": wallet,
	})
}

// checkout is the pure-GET checkout view. It either shows the user's
// existing pending order, or — if there is none — shows the cart contents
// with a "Place order" button that POSTs back to /checkout.
func (h *Handler) checkout(w http.ResponseWriter, r *http.Request) {
	wallet := strings.TrimSpace(r.URL.Query().Get("wallet"))
	if !isValidWallet(wallet) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	data := map[string]any{
		"Wallet":        wallet,
		"EthPriceUSD":   h.ethPrice,
		"RecipientAddr": h.ethWallet,
		"Error":         r.URL.Query().Get("err"),
	}

	o, err := h.orderRepo.FindPendingByWallet(r.Context(), wallet)
	switch {
	case err == nil:
		// Existing pending order — show the pay-with-MetaMask section.
		data["OrderID"] = o.ID.String()
		data["TotalUSD"] = o.TotalAmount
		data["EthAmount"] = o.TotalAmount / h.ethPrice
	case errors.Is(err, orders.ErrNotFound):
		// No pending order — preview the cart for confirmation.
		cartData := h.cartStore.Get(wallet)
		if len(cartData.Items) == 0 {
			http.Redirect(w, r, "/cart?wallet="+wallet, http.StatusSeeOther)
			return
		}
		var preview []map[string]any
		var total float64
		for _, item := range cartData.Items {
			a, gerr := h.articleRepo.Get(r.Context(), item.ArticleID)
			if gerr != nil {
				continue
			}
			line := a.Price * float64(item.Quantity)
			total += line
			preview = append(preview, map[string]any{
				"Name":      a.Name,
				"Quantity":  item.Quantity,
				"UnitPrice": a.Price,
				"LineTotal": line,
			})
		}
		data["Preview"] = preview
		data["TotalUSD"] = total
		data["EthAmount"] = total / h.ethPrice
	default:
		h.serverError(w, err)
		return
	}

	h.renderWithRequest(w, r, "customer/checkout.html", data)
}

// checkoutCreate handles the form POST that turns the cart into a pending order.
// It is the *only* place where /checkout has a side effect.
func (h *Handler) checkoutCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	wallet := strings.TrimSpace(r.FormValue("wallet_address"))
	if !isValidWallet(wallet) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// If the customer already has a pending order, skip recreation (idempotent
	// POST — protects against double-submit and accidental reload).
	if _, err := h.orderRepo.FindPendingByWallet(r.Context(), wallet); err == nil {
		http.Redirect(w, r, "/checkout?wallet="+wallet, http.StatusSeeOther)
		return
	} else if !errors.Is(err, orders.ErrNotFound) {
		h.serverError(w, err)
		return
	}

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

	if _, err := h.orderRepo.Create(r.Context(), input); err != nil {
		msg := fmt.Sprintf("Could not create order: %v", err)
		http.Redirect(w, r, "/checkout?wallet="+wallet+"&err="+url.QueryEscape(msg), http.StatusSeeOther)
		return
	}
	h.cartStore.Clear(wallet)
	http.Redirect(w, r, "/checkout?wallet="+wallet, http.StatusSeeOther)
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
	h.renderWithRequest(w, r, "admin/articles.html", map[string]any{
		"Articles": list,
		"Flash":    r.URL.Query().Get("flash"),
	})
}

func (h *Handler) adminArticleNew(w http.ResponseWriter, r *http.Request) {
	if !h.checkAdmin(w, r) {
		return
	}
	h.renderWithRequest(w, r, "admin/article_form.html", map[string]any{
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
		h.renderWithRequest(w, r, "admin/article_form.html", map[string]any{
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
	h.renderWithRequest(w, r, "admin/article_form.html", map[string]any{"IsEdit": true, "Article": a})
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
	h.renderWithRequest(w, r, "admin/orders.html", map[string]any{
		"Orders":     list,
		"Total":      total,
		"HasPrev":    offset > 0,
		"HasNext":    offset+pageSize < total,
		"PrevOffset": offset - pageSize,
		"NextOffset": offset + pageSize,
	})
}

// ── admin auth (browser cookie flow) ───────────────────────────────────────

const adminCookieName = "shop_admin"

func (h *Handler) adminLoginPage(w http.ResponseWriter, r *http.Request) {
	h.renderWithRequest(w, r, "admin/login.html", map[string]any{
		"Error": r.URL.Query().Get("err"),
	})
}

func (h *Handler) adminLoginSubmit(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	key := strings.TrimSpace(r.FormValue("admin_key"))
	if h.adminKey == "" || key != h.adminKey {
		http.Redirect(w, r, "/admin/login?err=Invalid+admin+key", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    key,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   8 * 60 * 60, // 8 hours
	})
	http.Redirect(w, r, "/admin/articles", http.StatusSeeOther)
}

func (h *Handler) adminLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// ── helpers ────────────────────────────────────────────────────────────────

func (h *Handler) checkAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h.adminKey == "" {
		return true
	}
	// API clients can use X-Admin-Key, browsers use the admin cookie.
	if r.Header.Get("X-Admin-Key") == h.adminKey {
		return true
	}
	if c, err := r.Cookie(adminCookieName); err == nil && c.Value == h.adminKey {
		return true
	}
	// JSON clients get a 401, browsers get redirected to the login page.
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
	return false
}

func (h *Handler) renderWithRequest(w http.ResponseWriter, r *http.Request, name string, data any) {
	tmpl := parse(name)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Inject IsAdmin so layout can show/hide admin chrome.
	m, ok := data.(map[string]any)
	if !ok {
		m = map[string]any{"_": data}
	}
	if _, set := m["IsAdmin"]; !set {
		m["IsAdmin"] = r != nil && h.isAdminRequest(r)
	}
	if err := tmpl.Execute(w, m); err != nil {
		h.logger.Error("render template", "name", name, "err", err)
	}
}

// isAdminRequest returns true if the request carries a valid admin credential.
// Unlike checkAdmin, it never writes to the response.
func (h *Handler) isAdminRequest(r *http.Request) bool {
	if h.adminKey == "" {
		return true
	}
	if r.Header.Get("X-Admin-Key") == h.adminKey {
		return true
	}
	if c, err := r.Cookie(adminCookieName); err == nil && c.Value == h.adminKey {
		return true
	}
	return false
}

func (h *Handler) serverError(w http.ResponseWriter, err error) {
	h.logger.Error("ui server error", "err", err)
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}
