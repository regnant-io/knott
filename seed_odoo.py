#!/usr/bin/env python3
"""Seed an Odoo (17/18/19) demo DB. Usage:
  export ODOO_LOGIN=you@example.com ODOO_API_KEY=xxxx [ODOO_DB=regnant] [SCALE=1]
  python seed_odoo.py
Amounts are plain numbers (treated as TZS-sized values; company currency is not changed).
"""
import os, random, sys, xmlrpc.client
from datetime import datetime, timedelta

URL = os.getenv("ODOO_URL", "https://regnant.odoo.com").strip()
DB = os.getenv("ODOO_DB", "regnant").strip()
LOGIN = os.environ.get("ODOO_LOGIN", "daudi.abinallah@gmail.com").strip()
KEY = os.environ.get("ODOO_API_KEY", "").strip()
if not KEY:
    sys.exit("Set ODOO_API_KEY before seeding Odoo")
SCALE = float(os.getenv("SCALE", "1"))
random.seed(42)

common = xmlrpc.client.ServerProxy(f"{URL}/xmlrpc/2/common")
uid = common.authenticate(DB, LOGIN, KEY, {})
if not uid:
    sys.exit("Authentication failed - check ODOO_LOGIN / ODOO_DB / key")
VER = common.version()["server_version_info"][0]
models = xmlrpc.client.ServerProxy(f"{URL}/xmlrpc/2/object", allow_none=True)
print("Connected, Odoo major version", VER)

def x(model, method, *args, **kw):
    return models.execute_kw(DB, uid, KEY, model, method, list(args), kw)

def goc(model, domain, vals):
    r = x(model, "search", domain, limit=1)
    return r[0] if r else x(model, "create", vals)

def batch(model, vals, size=40, **kw):
    ids = []
    for i in range(0, len(vals), size):
        ids += x(model, "create", vals[i:i + size], **kw)
    return ids

def step(name, fn):
    try:
        fn(); print("OK  ", name)
    except Exception as e:
        print("SKIP", name, "->", str(e)[:200])

now = datetime.now()
fmt = lambda d: d.strftime("%Y-%m-%d %H:%M:%S")
company = x("res.company", "search", [], limit=1)[0]
tz = x("res.country", "search", [("code", "=", "TZ")], limit=1)
tz = tz[0] if tz else False

# ---------- Vendors & customers ----------
VENDORS = [
    ("Kilimanjaro Industrial Supplies Ltd", "Dar es Salaam"), ("Dar Bearings & Tools Co.", "Dar es Salaam"),
    ("Mwanza Hydraulics Ltd", "Mwanza"), ("Arusha Auto Spares", "Arusha"),
    ("Tanga Electrical Wholesalers", "Tanga"), ("Dodoma Fasteners & Fittings", "Dodoma"),
    ("Mbeya Heavy Parts Ltd", "Mbeya"), ("Pwani Quick Supplies Ltd", "Kibaha"),
    ("Quickbuy General Traders", "Dar es Salaam"),
]
V = []
for n, city in VENDORS:
    slug = n.lower().split()[0]
    V.append(goc("res.partner", [("name", "=", n)], dict(
        name=n, is_company=True, city=city, country_id=tz, supplier_rank=1,
        email=f"sales@{slug}.co.tz", phone=f"+255 7{random.randint(10,99)} {random.randint(100,999)} {random.randint(100,999)}")))
x("res.partner", "message_post", [V[8]], body="Dispute: short delivery on 3 consignments in 2026, credit note still outstanding. Quality complaint on bearings batch 2026-05. Payment terms: 30 days.")
x("res.partner", "message_post", [V[7]], body="New supplier onboarded Oct 2026. No transaction history yet. Payment terms: advance.")

CUST = ["Geita Gold Mining", "Kahama Cement Works", "Morogoro Sugar Estates", "Songea Agro Processors", "Lindi Quarry Co.",
        "Tabora Tobacco Cooperative", "Iringa Timber Mills", "Mtwara Port Services", "Shinyanga Diamond Ltd", "Kilombero Rice Farms",
        "Dar Bottlers Ltd", "Zanzibar Marine Works", "Singida Wind Power", "Ruvuma Coal Mining", "Kigoma Fisheries Co-op",
        "Arusha Coffee Estates", "Mara Roads & Bridges", "Njombe Tea Factory", "Tanga Cement PLC", "Rukwa Salt Works"]
CUST = (CUST * max(1, int(SCALE * 2)))[: int(len(CUST) * SCALE) + 10]
cust_ids = []
for i, n in enumerate(CUST):
    n = n if i < 20 else f"{n} #{i//20+1}"
    cust_ids.append(goc("res.partner", [("name", "=", n)], dict(
        name=n, is_company=True, customer_rank=1, country_id=tz, city=random.choice(["Dar es Salaam", "Mwanza", "Arusha", "Dodoma", "Mbeya"]),
        email=f"procurement@{n.split()[0].lower()}.co.tz")))

# ---------- Products ----------
cat = lambda n: goc("product.category", [("name", "=", n)], dict(name=n))
CAT = {k: cat(v) for k, v in dict(BRG="Bearings", FLT="Filters", BLT="Fasteners", HSE="Hydraulic Hoses",
                                  BRK="Brakes", ELC="Electrical", FG="Finished Machines").items()}
specs = []
for n in [6000, 6002, 6004, 6200, 6202, 6204, 6205, 6206, 6208, 6210, 6304, 6306, 6308, 6310, 6312, 6314]:
    specs.append((f"BRG-{n}", f"Ball bearing {n}-2RS", "BRG", random.randint(8000, 45000)))
for t, tn in [("OIL", "Oil"), ("AIR", "Air"), ("FUEL", "Fuel"), ("HYD", "Hydraulic")]:
    for s in "ABC":
        specs.append((f"FLT-{t}-{s}", f"{tn} filter type {s}", "FLT", random.randint(25000, 180000)))
for d in [8, 10, 12, 14, 16, 20, 24, 30]:
    for l in [30, 50, 80, 120]:
        specs.append((f"BLT-M{d}x{l}", f"Hex bolt M{d}x{l} gr 8.8 (box of 100)", "BLT", random.randint(250, 400) * d))
for sz in ["06", "08", "12", "16", "20"]:
    for l in [1, 2, 3, 4]:
        specs.append((f"HSE-R2-{sz}-{l}M", f"Hydraulic hose R2 DN{sz} x {l} m", "HSE", int(sz) * l * random.randint(3500, 6000)))
for m in ["HL120", "HL150", "TX200", "TX250", "DZ90"]:
    specs.append((f"BRK-PAD-{m}", f"Brake pad set {m}", "BRK", random.randint(90000, 350000)))
for c, n, p in [("ELC-CNT-25A", "Contactor 25A 3P", 48000), ("ELC-CNT-40A", "Contactor 40A 3P", 82000),
                ("ELC-CBL-4MM", "Cable 4mm2 (100 m roll)", 210000), ("ELC-CBL-10MM", "Cable 10mm2 (100 m roll)", 480000),
                ("ELC-SW-LMT", "Limit switch IP67", 36000), ("ELC-MTR-5K", "Motor 5.5kW 4P", 1250000)]:
    specs.append((c, n, "ELC", p))

fam_vendors = {"BRG": [0, 1, 6, 8], "BLT": [0, 1, 5, 7], "FLT": [0, 2, 3, 8], "HSE": [2, 5, 6], "BRK": [3, 6], "ELC": [4, 8]}
ptype = dict(type="consu", is_storable=True) if VER >= 18 else dict(type="product")
bc = [600000000000]
def barcode():
    bc[0] += random.randint(1, 97); return str(bc[0])

existing = {r["default_code"] for r in x("product.template", "search_read", [("default_code", "!=", False)], fields=["default_code"])}
vals = []
for code, name, fam, cost in specs:
    if code in existing: continue
    sellers = [(0, 0, dict(partner_id=V[i], price=round(cost * random.uniform(0.95, 1.08)), min_qty=1, delay=random.randint(3, 21)))
               for i in random.sample(fam_vendors[fam], k=min(2, len(fam_vendors[fam])))]
    vals.append(dict(name=name, default_code=code, categ_id=CAT[fam], standard_price=cost, list_price=round(cost * 1.35),
                     purchase_ok=True, sale_ok=True, barcode=barcode(), seller_ids=sellers, **ptype))
if "NOV-ADAPT-0099" not in existing:   # product with NO vendor (for the 'no vendor' branch)
    vals.append(dict(name="Custom flange adapter (no vendor yet)", default_code="NOV-ADAPT-0099", categ_id=CAT["HSE"],
                     standard_price=95000, list_price=140000, barcode=barcode(), **ptype))
batch("product.template", vals)

prods = x("product.product", "search_read", [("default_code", "!=", False)], fields=["default_code", "standard_price", "name", "product_tmpl_id"])
pid = {p["default_code"]: p for p in prods}
print("Products:", len(pid))

# ---------- Stock ----------
def stock():
    wh = x("stock.warehouse", "search_read", [], fields=["lot_stock_id"], limit=1)[0]
    loc = wh["lot_stock_id"][0]
    q = [dict(product_id=p["id"], location_id=loc, inventory_quantity=random.choice([0, 3, 8, 15, 40, 120, 300, 800]))
         for c, p in pid.items() if c != "NOV-ADAPT-0099"]
    ids = batch("stock.quant", q, context={"inventory_mode": True})
    x("stock.quant", "action_apply_inventory", ids)
    ops = [dict(product_id=p["id"], location_id=loc, warehouse_id=wh["id"] if "id" in wh else False,
                product_min_qty=20, product_max_qty=150) for c, p in list(pid.items())[:30]]
    batch("stock.warehouse.orderpoint", ops)
step("stock + reorder rules", stock)

# ---------- Manufacturing: finished goods + BoMs ----------
def mrp():
    comps = [p for c, p in pid.items() if c[:3] in ("BRG", "FLT", "BLT", "HSE", "ELC")]
    for code, name in [("FG-PUMP-100", "Centrifugal pump unit CP-100"), ("FG-PUMP-200", "Centrifugal pump unit CP-200"),
                       ("FG-CONV-50", "Belt conveyor module BC-50"), ("FG-HPU-30", "Hydraulic power unit HPU-30"),
                       ("FG-GEN-45", "Diesel genset enclosure GE-45")]:
        tid = goc("product.template", [("default_code", "=", code)], dict(name=name, default_code=code, categ_id=CAT["FG"],
                  standard_price=random.randint(1500000, 9000000), list_price=random.randint(2500000, 12000000), barcode=barcode(), **ptype))
        lines = [(0, 0, dict(product_id=c["id"], product_qty=random.randint(1, 8))) for c in random.sample(comps, random.randint(4, 7))]
        goc("mrp.bom", [("product_tmpl_id", "=", tid)], dict(product_tmpl_id=tid, product_qty=1, bom_line_ids=lines))
step("manufacturing BoMs", mrp)

# ---------- Employees ----------
def emp():
    depts = {n: goc("hr.department", [("name", "=", n)], dict(name=n)) for n in ["Purchasing", "Warehouse", "Production", "Sales", "Finance"]}
    first = ["Amina", "Juma", "Neema", "Baraka", "Salma", "Emmanuel", "Zawadi", "Hassan", "Grace", "Rashid", "Upendo", "Daudi", "Fatuma", "Peter", "Mariam"]
    last = ["Mushi", "Kimaro", "Mwakyusa", "Massawe", "Mtui", "Shayo", "Lyimo", "Mbwana", "Komba", "Nyerere"]
    jobs = {"Purchasing": "Buyer", "Warehouse": "Storekeeper", "Production": "Machine Operator", "Sales": "Sales Executive", "Finance": "Accountant"}
    for i in range(int(25 * SCALE)):
        d = random.choice(list(depts)); n = f"{random.choice(first)} {random.choice(last)}"
        goc("hr.employee", [("name", "=", n)], dict(name=n, job_title=jobs[d], department_id=depts[d],
            work_email=f"{n.lower().replace(' ', '.')}@regnant.co.tz"))
step("employees", emp)

# ---------- CRM ----------
def crm():
    stages = x("crm.stage", "search", [])
    needs = ["spare parts framework agreement", "pump refurbishment", "conveyor upgrade", "annual bearing supply", "hydraulic retrofit",
             "genset enclosure order", "maintenance contract", "emergency filter restock"]
    leads = []
    for i in range(int(120 * SCALE)):
        c = random.choice(CUST)
        leads.append(dict(name=f"{c} - {random.choice(needs)}", partner_name=c, contact_name=f"{random.choice(['Mr.', 'Ms.'])} {random.choice(['Mushi','Kimaro','Lyimo','Komba'])}",
                          email_from=f"buyer{i}@{c.split()[0].lower()}.co.tz", phone=f"+255 7{random.randint(10,99)} {random.randint(100,999)} {random.randint(100,999)}",
                          type="opportunity", stage_id=random.choice(stages), expected_revenue=random.choice([2e6, 5e6, 8.5e6, 15e6, 40e6, 90e6]),
                          priority=random.choice(["0", "1", "2", "3"]), date_deadline=(now + timedelta(days=random.randint(5, 120))).strftime("%Y-%m-%d")))
    batch("crm.lead", leads)
step("CRM opportunities", crm)

# ---------- Purchasing ----------
x("res.company", "write", [company], dict(po_double_validation="two_step", po_double_validation_amount=1000000))
vendor_items = {}
for p in prods:
    for s in x("product.supplierinfo", "search_read", [("product_tmpl_id", "=", p["product_tmpl_id"][0])], fields=["partner_id", "price"]):
        vendor_items.setdefault(s["partner_id"][0], []).append((p["id"], s["price"], p["name"]))

def lines(v, target, mult=1.0, n=3):
    items = random.sample(vendor_items[v], min(n, len(vendor_items[v])))
    out = []
    for p, price, name in items:
        price = round(price * mult)
        out.append((0, 0, dict(product_id=p, name=name, product_qty=max(1, round(target / len(items) / price)), price_unit=price,
                               date_planned=fmt(now + timedelta(days=7)))))
    return out

def po(v, target, days_ago=0, mult=1.0, n=3, approve=False):
    oid = x("purchase.order", "create", dict(partner_id=v, date_order=fmt(now - timedelta(days=days_ago)), order_line=lines(v, target, mult, n)))
    x("purchase.order", "button_confirm", [oid])
    if approve and x("purchase.order", "read", [oid], fields=["state"])[0]["state"] == "to approve":
        x("purchase.order", "button_approve", [oid])
    return oid

def purchasing():
    hist = [V[i] for i in (0, 1, 2, 3, 4, 5, 6, 8)]
    for _ in range(int(90 * SCALE)):                  # history: mostly normal, approved
        po(random.choice(hist), random.choice([400000, 900000, 1600000, 2500000, 4000000, 7500000]),
           days_ago=random.randint(10, 360), mult=random.uniform(0.97, 1.05), approve=True)
step("historical purchase orders", purchasing)

def pending():                                         # the 8 'To Approve' items for workflow 1
    S = [("1 routine, trusted vendor", V[0], 1_800_000, 1.0), ("2 routine, trusted vendor", V[1], 2_600_000, 1.0),
         ("3 routine, bigger", V[6], 4_100_000, 1.0), ("4 OVER LIMIT 18.5M", V[2], 18_500_000, 1.0),
         ("5 OVER LIMIT 42M", V[6], 42_000_000, 1.0), ("6 new vendor, no history", V[7], 3_200_000, 1.0),
         ("7 price ~3x normal", V[1], 2_000_000, 3.0), ("8 vendor with open disputes", V[8], 2_200_000, 1.0)]
    for label, v, t, m in S:
        oid = po(v, t, mult=m)
        r = x("purchase.order", "read", [oid], fields=["name", "state", "amount_total"])[0]
        print(f"   pending {label}: {r['name']} {r['state']} {r['amount_total']:.0f}")
step("8 pending POs (To Approve)", pending)

# ---------- Invoicing ----------
def inv():
    for i in range(int(60 * SCALE)):
        d = (now - timedelta(days=random.randint(1, 300))).strftime("%Y-%m-%d")
        mid = x("account.move", "create", dict(move_type="out_invoice", partner_id=random.choice(cust_ids), invoice_date=d,
                invoice_line_ids=[(0, 0, dict(name=random.choice(["Spare parts supply", "Pump servicing", "Hydraulic retrofit", "Freight & handling"]),
                                              quantity=random.randint(1, 10), price_unit=random.choice([85000, 240000, 560000, 1200000])))]))
        x("account.move", "action_post", [mid])
step("customer invoices", inv)
print("Done.")
