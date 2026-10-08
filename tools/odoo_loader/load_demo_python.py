import xmlrpc.client
import random
import uuid
import sys
import os

# --- CONFIGURATION ---
URL = 'https://regnant.odoo.com'
DB = 'regnant'
# The user must provide the login email for the Odoo.com instance
# Since I don't have it, I'll make it a variable they can edit or I'll try a common pattern
USERNAME = 'admin'
API_KEY = os.environ.get('ODOO_API_KEY', '').strip()
if not API_KEY:
    sys.exit('Set ODOO_API_KEY before loading Odoo demo data')

NUM_VENDORS = 10
NUM_PRODUCTS = 50
NUM_ORDERS = 200

def connect():
    print(f"Connecting to {URL}...")
    common = xmlrpc.client.ServerProxy(f'{URL}/xmlrpc/2/common')
    try:
        uid = common.authenticate(DB, USERNAME, API_KEY, {})
        if not uid:
            raise Exception("Authentication failed: UID returned was empty")
        print(f"Successfully authenticated. UID: {uid}")
        models = xmlrpc.client.ServerProxy(f'{URL}/xmlrpc/2/object')
        return uid, models
    except Exception as e:
        print(f"Connection failed: {e}")
        sys.exit(1)

def load_data():
    uid, models = connect()

    # 1. Create Products
    # In Odoo, we create 'product.template' which then creates 'product.product'
    print(f"Creating {NUM_PRODUCTS} products...")
    product_ids = []
    product_names = ["A4 Paper", "Toner Cartridge", "Ergonomic Chair", "Desk Lamp", "Laptop Stand", "Wireless Mouse", "Mechanical Keyboard", "HDMI Cable", "USB-C Hub", "Whiteboard Markers"]

    for i in range(NUM_PRODUCTS):
        name = f"{random.choice(product_names)} {i+1}"
        try:
            p_id = models.execute_kw(DB, uid, API_KEY, 'product.template', 'create', [{
                'name': name,
                'list_price': random.uniform(100, 5000),
                'standard_price': random.uniform(50, 3000),
            }])
            # We need the product.product ID for PO lines
            prod_prod = models.execute_kw(DB, uid, API_KEY, 'product.product', 'search', [[['name', '=', name]]])
            if prod_prod:
                product_ids.append(prod_prod[0])
        except Exception as e:
            print(f"Error creating product {name}: {e}")

    # 2. Create Vendors
    print(f"Creating {NUM_VENDORS} vendors...")
    vendor_ids = []
    vendor_names = ["Global Supplies Ltd", "Tanzania Tech Corp", "East Africa Paper Co", "Safari Logistics", "Zanzibar Office Wear", "Dar Electronics", "Kilimanjaro Stationery", "Mwanza Industrial", "Arusha Trading", "Dodoma Office Sol"]
    for name in vendor_names:
        try:
            v_id = models.execute_kw(DB, uid, API_KEY, 'res.partner', 'create', [{
                'name': name,
                'is_company': True,
            }])
            vendor_ids.append(v_id)
        except Exception as e:
            print(f"Error creating partner {name}: {e}")

    # 3. Create Purchase Orders
    print(f"Creating {NUM_ORDERS} purchase orders...")
    for i in range(NUM_ORDERS):
        dice = random.randint(0, 100)
        if dice < 60:
            amount = random.uniform(1000, 500000) # Routine
        elif dice < 80:
            amount = random.uniform(15000000, 30000000) # High Value
        else:
            amount = random.uniform(800000, 2000000) # Anomalous

        try:
            po_id = models.execute_kw(DB, uid, API_KEY, 'purchase.order', 'create', [{
                'partner_id': random.choice(vendor_ids),
                'amount_total': amount,
                # 'state' is usually controlled by buttons, but for demo data we force it
                # In Odoo 17+, the field is often 'state'
            }])

            # Add lines
            lines = []
            for _ in range(random.randint(1, 3)):
                lines.append((0, 0, {
                    'product_id': random.choice(product_ids),
                    'product_qty': random.randint(1, 10),
                    'price_unit': amount / 3,
                }))

            models.execute_kw(DB, uid, API_KEY, 'purchase.order', 'write', [[po_id], {
                'order_line': lines
            }])

            # Force state to 'to approve' (draft -> sent -> to approve depends on settings)
            # For many Odoo configs, 'draft' is the start.
            models.execute_kw(DB, uid, API_KEY, 'purchase.order', 'write', [[po_id], {'state': 'sent'}])

        except Exception as e:
            print(f"Error creating PO {i}: {e}")

    print("\n✅ Data load complete!")

if __name__ == "__main__":
    load_data()
