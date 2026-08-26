#!/usr/bin/env python3
"""Register users and place orders"""
import requests
import time

API = "http://localhost:8099/api/v1"
PAIR = "BTC_USDT"

def register(email, password="password123"):
    r = requests.post(f"{API}/users/register",
                       json={"email": email, "password": password})
    return r.status_code

def login(email, password="password123"):
    r = requests.post(f"{API}/users/login",
                       json={"email": email, "password": password})
    if r.status_code != 200:
        return None
    return r.json().get("token")

def place_order(token, side, price, qty):
    r = requests.post(f"{API}/orders",
                      headers={"Authorization": f"Bearer {token}"},
                      json={"pair": PAIR, "side": side,
                            "price": str(price), "quantity": str(qty)})
    return r.status_code, r.text[:80]

# First, register 20 sellers
print("=== Registering sellers ===")
for i in range(1, 21):
    email = f"seller{i}@test.local"
    code = register(email)
    print(f"  {email}: {code}")

# Now login each and place ASKs
print()
print("=== Placing ASKS ===")
ask_prices = [29800, 29750, 29700, 29650, 29600, 29580, 29550, 29520, 29500, 29850, 29900]
ask_qtys = [0.5, 1.2, 0.3, 2.0, 0.8, 1.5, 0.4, 0.7, 3.0, 0.6, 1.0]

for i, (price, qty) in enumerate(zip(ask_prices, ask_qtys)):
    email = f"seller{i+1}@test.local"
    token = login(email)
    if not token:
        print(f"  SKIP {email}")
        continue
    code, body = place_order(token, "SELL", price, qty)
    print(f"  ASK {price} x {qty} by {email} -> {code} {body[:40]}")

# Place BIDS
print()
print("=== Placing BIDS ===")
bid_prices = [29400, 29350, 29300, 29280, 29250, 29200, 29150, 29100, 29000, 28900]
bid_qtys = [2.0, 1.0, 3.0, 0.5, 1.5, 2.5, 0.8, 4.0, 1.2, 3.5]

for i, (price, qty) in enumerate(zip(bid_prices, bid_qtys)):
    email = "t@t.com" if i % 2 == 0 else "boss@goexchange.local"
    token = login(email)
    if not token:
        continue
    code, body = place_order(token, "BUY", price, qty)
    print(f"  BID {price} x {qty} by {email} -> {code}")

print()
print("=== Final Order Book ===")
admin_token = login("boss@goexchange.local")
r = requests.get(f"{API}/markets/BTC/USDT/orderbook",
                 headers={"Authorization": f"Bearer {admin_token}"})
d = r.json()
print(f"BIDS ({len(d['bids'])} levels):")
for b in d['bids'][:12]:
    print(f"  {float(b['price']):>10.2f} x {float(b['quantity']):>6.4f}")
print(f"ASKS ({len(d['asks'])} levels):")
for a in d['asks'][:12]:
    print(f"  {float(a['price']):>10.2f} x {float(a['quantity']):>6.4f}")
