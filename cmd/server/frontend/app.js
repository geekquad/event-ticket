// Use same origin when served from API server; fallback for direct file open.
const API = window.location.origin || `http://${window.location.hostname}:8085`;

  // ── State ──────────────────────────────────────────────────────────────────
  let currentUserID = localStorage.getItem('userId') || '';
  let eventsCache   = [];
  let countdownIntervals = {};

  // ── Helpers ────────────────────────────────────────────────────────────────
  function showToast(msg, type = 'success') {
    const t = document.getElementById('toast');
    t.textContent = msg;
    t.className = `show ${type}`;
    setTimeout(() => { t.className = ''; }, 3500);
  }

  function activateTab(name) {
    document.querySelectorAll('.page').forEach(p => p.classList.remove('active'));
    document.querySelectorAll('nav button').forEach(b => b.classList.remove('active'));
    document.getElementById(`page-${name}`).classList.add('active');
    document.querySelector(`nav button[data-page="${name}"]`).classList.add('active');
    localStorage.setItem('activeTab', name);
  }

  function showPage(name) {
    activateTab(name);
    if (name === 'bookings') loadMyBookings();
    if (name === 'events')   loadEvents();
  }

  async function apiFetch(path, options = {}) {
    const headers = { 'Content-Type': 'application/json', ...(options.headers || {}) };
    if (currentUserID) headers['X-User-ID'] = currentUserID;
    const res = await fetch(`${API}${path}`, { ...options, headers });
    if (res.status === 204) return null;
    const body = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
    return body;
  }

  function escHtml(str) {
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  // ── Users ──────────────────────────────────────────────────────────────────
  async function loadUsers() {
    try {
      const data = await apiFetch('/users');
      const sel = document.getElementById('userSelect');
      (data.users || []).forEach(u => {
        const opt = document.createElement('option');
        opt.value = u.id;
        opt.textContent = u.name;
        sel.appendChild(opt);
      });
      // Restore previously selected user
      if (currentUserID) {
        sel.value = currentUserID;
        if (!sel.value) {
          // Saved ID no longer exists in the list
          currentUserID = '';
          localStorage.removeItem('userId');
        }
        loadEvents();
      }
    } catch (e) {
      console.error('Failed to load users', e);
    }
  }

  document.getElementById('userSelect').addEventListener('change', e => {
    currentUserID = e.target.value;
    if (currentUserID) {
      localStorage.setItem('userId', currentUserID);
    } else {
      localStorage.removeItem('userId');
    }
    loadEvents();
  });

  // ── Events ─────────────────────────────────────────────────────────────────
  async function loadEvents() {
    const container = document.getElementById('events-container');
    container.innerHTML = '<div class="spinner"></div>';
    try {
      const data = await apiFetch('/events');
      eventsCache = data.events || [];
      renderEvents(eventsCache);
    } catch (e) {
      container.innerHTML = `<p class="empty-state">Failed to load events: ${escHtml(e.message)}</p>`;
    }
  }

  function renderEvents(events) {
    const container = document.getElementById('events-container');
    if (!events || events.length === 0) {
      container.innerHTML = '<p class="empty-state">No events found.</p>';
      return;
    }
    container.innerHTML = '';
    const grid = document.createElement('div');
    grid.className = 'events-grid';
    events.forEach(ev => grid.appendChild(buildEventCard(ev)));
    container.appendChild(grid);
  }

  function buildEventCard(ev) {
    const capacity  = ev.venue?.capacity ?? 0;
    const isSoldOut = ev.availableCount === 0;
    const booked    = capacity - ev.availableCount;
    const pct       = capacity > 0 ? Math.round((booked / capacity) * 100) : 0;
    const fillClass = pct >= 100 ? 'full' : pct >= 75 ? 'warn' : '';
    const date      = new Date(ev.dateTime).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });

    const card = document.createElement('div');
    card.className = 'event-card';
    card.innerHTML = `
      <h3>${escHtml(ev.name)}</h3>
      <p class="meta">📅 ${date}</p>
      <p class="venue">📍 ${escHtml(ev.venue?.name || '')}</p>
      <div class="capacity-label">
        <span>${isSoldOut
          ? '<span class="badge-sold-out">Sold out</span>'
          : `${ev.availableCount} seat${ev.availableCount !== 1 ? 's' : ''} available`
        }</span>
        <span>${booked} / ${capacity} booked</span>
      </div>
      <div class="capacity-bar">
        <div class="capacity-bar-fill ${fillClass}" style="width:${pct}%"></div>
      </div>
    `;

    // Quantity stepper + reserve button
    const reserveRow = document.createElement('div');
    reserveRow.className = 'reserve-row';

    let quantity = 1;

    if (!isSoldOut) {
      const stepper = document.createElement('div');
      stepper.className = 'qty-stepper';
      const qtySpan = document.createElement('span');
      qtySpan.textContent = '1';

      const dec = document.createElement('button');
      dec.textContent = '−';
      dec.onclick = () => {
        if (quantity > 1) { quantity--; qtySpan.textContent = quantity; }
      };
      const inc = document.createElement('button');
      inc.textContent = '+';
      inc.onclick = () => {
        if (quantity < (ev.availableCount ?? 0)) { quantity++; qtySpan.textContent = quantity; }
      };

      stepper.appendChild(dec);
      stepper.appendChild(qtySpan);
      stepper.appendChild(inc);
      reserveRow.appendChild(stepper);
    }

    const btn = document.createElement('button');
    btn.className = 'btn btn-primary';
    btn.textContent = isSoldOut ? 'Sold Out' : 'Reserve';
    btn.disabled    = isSoldOut || !currentUserID;
    btn.title       = !currentUserID ? 'Select a user first' : '';
    btn.onclick     = () => reserveEvent(ev.id, quantity, btn);
    reserveRow.appendChild(btn);

    card.appendChild(reserveRow);
    return card;
  }

  async function reserveEvent(eventID, quantity, btn) {
    if (!currentUserID) { showToast('Please select a user first', 'error'); return; }
    btn.disabled    = true;
    btn.textContent = 'Reserving…';
    try {
      await apiFetch('/booking/reserve', {
        method: 'POST',
        body:   JSON.stringify({ eventId: eventID, quantity }),
      });
      showToast(`${quantity} seat${quantity > 1 ? 's' : ''} reserved! Confirm within the given time.`, 'success');
      await loadEvents();
      showPage('bookings');
    } catch (e) {
      showToast(`Reservation failed: ${e.message}`, 'error');
      btn.disabled    = false;
      btn.textContent = 'Reserve';
    }
  }

  // ── My Bookings ────────────────────────────────────────────────────────────
  function clearAllCountdowns() {
    Object.values(countdownIntervals).forEach(clearInterval);
    countdownIntervals = {};
  }

  async function loadMyBookings() {
    clearAllCountdowns();
    const container = document.getElementById('bookings-container');
    if (!currentUserID) {
      container.innerHTML = '<p class="empty-state">Select a user to view bookings.</p>';
      return;
    }
    container.innerHTML = '<div class="spinner"></div>';
    try {
      const data = await apiFetch('/booking/mine');
      renderBookings(data.bookings || []);
    } catch (e) {
      container.innerHTML = `<p class="empty-state">Failed to load bookings: ${escHtml(e.message)}</p>`;
    }
  }

  function renderBookings(bookings) {
    clearAllCountdowns();
    const container = document.getElementById('bookings-container');
    if (!bookings.length) {
      container.innerHTML = '<p class="empty-state">No active bookings.</p>';
      return;
    }

    const list = document.createElement('div');
    list.className = 'booking-list';

    bookings.forEach(b => {
      const ev        = eventsCache.find(e => e.id === b.eventId);
      const eventName = ev ? ev.name : `Event ${b.eventId.slice(0, 8)}…`;
      const eventDate = ev
        ? new Date(ev.dateTime).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
        : '';

      const isReserved  = b.status === 'RESERVED';
      const isConfirmed = b.status === 'CONFIRMED';

      const item = document.createElement('div');
      item.className = `booking-item ${isReserved ? 'reserved' : isConfirmed ? 'confirmed' : ''}`;

      let badgeHtml = '';
      if (isReserved)       badgeHtml = '<span class="badge badge-reserved-label">Reserved</span>';
      else if (isConfirmed) badgeHtml = '<span class="badge badge-confirmed-label">Confirmed</span>';

      const seatCount = b.quantity ?? 1;

      item.innerHTML = `
        <div class="info">
          <h3>${escHtml(eventName)} ${badgeHtml}</h3>
          ${eventDate ? `<p>📅 ${eventDate}</p>` : ''}
          <p>${seatCount} seat${seatCount !== 1 ? 's' : ''}</p>
          ${isReserved ? `<div class="countdown" id="countdown-${b.id}">⏰ calculating…</div>` : ''}
          <div class="booking-id">#${b.id}</div>
        </div>
        <div class="booking-actions" id="actions-${b.id}"></div>
      `;

      list.appendChild(item);

      const actions = item.querySelector(`#actions-${b.id}`);

      if (isReserved) {
        const confirmBtn = document.createElement('button');
        confirmBtn.className = 'btn-sm btn btn-success';
        confirmBtn.textContent = 'Confirm Purchase';
        confirmBtn.onclick = () => confirmBooking(b.id, confirmBtn);
        actions.appendChild(confirmBtn);
        startCountdown(b, confirmBtn);
      }

      if (isReserved || isConfirmed) {
        const cancelBtn = document.createElement('button');
        cancelBtn.className = 'btn-sm btn btn-outline';
        cancelBtn.textContent = 'Cancel';
        cancelBtn.onclick = () => cancelBooking(b.id, cancelBtn);
        actions.appendChild(cancelBtn);
      }
    });

    container.innerHTML = '';
    container.appendChild(list);
  }

  function startCountdown(booking, confirmBtn) {
    const expiresAt = new Date(booking.expiresAt).getTime();

    function tick() {
      const el = document.getElementById(`countdown-${booking.id}`);
      if (!el) { clearInterval(countdownIntervals[booking.id]); return; }

      const remaining = expiresAt - Date.now();
      if (remaining <= 0) {
        el.textContent = '⏰ Reservation expired';
        el.className   = 'countdown expired';
        if (confirmBtn) confirmBtn.disabled = true;
        clearInterval(countdownIntervals[booking.id]);
        return;
      }

      const mins = Math.floor(remaining / 60000);
      const secs = Math.floor((remaining % 60000) / 1000);
      el.textContent = `⏰ ${mins}:${String(secs).padStart(2, '0')} to confirm`;
      el.className   = `countdown${remaining < 60000 ? ' urgent' : ''}`;
    }

    tick();
    countdownIntervals[booking.id] = setInterval(tick, 1000);
  }

  async function confirmBooking(bookingID, btn) {
    btn.disabled    = true;
    btn.textContent = 'Confirming…';
    try {
      await apiFetch('/booking/confirm', {
        method: 'POST',
        body:   JSON.stringify({ bookingId: bookingID, paymentDetails: null }),
      });
      showToast('Booking confirmed!', 'success');
      await Promise.all([loadMyBookings(), loadEvents()]);
    } catch (e) {
      showToast(`Confirmation failed: ${e.message}`, 'error');
      btn.disabled    = false;
      btn.textContent = 'Confirm Purchase';
    }
  }

  async function cancelBooking(bookingID, btn) {
    btn.disabled    = true;
    btn.textContent = 'Cancelling…';
    try {
      await apiFetch(`/booking/${bookingID}`, { method: 'DELETE' });
      showToast('Booking cancelled. Seats released.', 'success');
      await Promise.all([loadMyBookings(), loadEvents()]);
    } catch (e) {
      showToast(`Cancel failed: ${e.message}`, 'error');
      btn.disabled    = false;
      btn.textContent = 'Cancel';
    }
  }

  // ── Boot ───────────────────────────────────────────────────────────────────
  loadUsers();        // restores user dropdown; calls loadEvents() when selection is applied
  loadEvents();       // always pre-populate eventsCache (needed for booking event names)

const savedTab = localStorage.getItem('activeTab') || 'events';
activateTab(savedTab);
if (savedTab === 'bookings') loadMyBookings();
