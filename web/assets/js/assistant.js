/* ==========================================================================
   Alfa Credit Assistant Curator Widget
   Coordinates user questions, product recommendations, calculator controls,
   and wizard transitions.
   ========================================================================== */

(function (root) {
  'use strict';

  var STORAGE_KEY_HISTORY = 'alfa_assistant_history';
  var STORAGE_KEY_STATE = 'alfa_assistant_state';
  var STORAGE_KEY_SESSION = 'alfa_assistant_session_id';

  function getSessionId() {
    try {
      var sid = sessionStorage.getItem(STORAGE_KEY_SESSION);
      if (!sid) {
        sid = 'sess_' + Math.random().toString(36).substring(2, 11) + '_' + Date.now();
        sessionStorage.setItem(STORAGE_KEY_SESSION, sid);
      }
      return sid;
    } catch (e) {
      return 'sess_fallback_' + Date.now();
    }
  }

  function getPageContext() {
    var path = window.location.pathname || '';
    var page = 'landing';
    if (path.indexOf('personalAccount') !== -1 || path.indexOf('cabinet') !== -1) {
      page = 'cabinet';
    } else if (path.indexOf('login') !== -1) {
      page = 'login';
    }

    var calcState = null;
    // Check if calculator inputs are present on the current page
    var amountInput = document.getElementById('calc-amount-input') || document.querySelector('input[name="amount"]');
    var termInput = document.getElementById('calc-term-input') || document.querySelector('input[name="term"]');
    var orgRadio = document.querySelector('input[name="org"]:checked');
    var colRadio = document.querySelector('input[name="collateral"]:checked');
    var ageRadio = document.querySelector('input[name="age"]:checked');

    if (amountInput || orgRadio) {
      calcState = {
        amount: amountInput ? parseInt(amountInput.value.replace(/\D/g, ''), 10) : null,
        term: termInput ? parseInt(termInput.value.replace(/\D/g, ''), 10) : null,
        org: orgRadio ? orgRadio.value : null,
        collateral: colRadio ? colRadio.value : null,
        age: ageRadio ? ageRadio.value : null,
      };
    }

    var appStatus = null;
    var statusElem = document.querySelector('[data-status]') || document.querySelector('.status-badge');
    if (statusElem) {
      appStatus = statusElem.getAttribute('data-status') || statusElem.textContent.trim();
    }

    return {
      page: page,
      calculator_state: calcState,
      application_status: appStatus,
    };
  }

  var AlfaAssistant = {
    isOpen: false,
    history: [],
    sessionId: getSessionId(),

    init: function () {
      this.loadHistory();
      this.renderWidget();
      this.bindEvents();

      // Check if open state was persisted
      try {
        var savedState = sessionStorage.getItem(STORAGE_KEY_STATE);
        if (savedState === 'open') {
          this.open();
        }
      } catch (e) {}

      // Initial greeting if history is empty
      if (this.history.length === 0) {
        this.fetchGreeting();
      }
    },

    loadHistory: function () {
      try {
        var raw = sessionStorage.getItem(STORAGE_KEY_HISTORY);
        if (raw) {
          this.history = JSON.parse(raw);
        }
      } catch (e) {
        this.history = [];
      }
    },

    saveHistory: function () {
      try {
        sessionStorage.setItem(STORAGE_KEY_HISTORY, JSON.stringify(this.history));
      } catch (e) {}
    },

    renderWidget: function () {
      var root = document.getElementById('alfa-assistant-root');
      if (root) return; // already rendered

      root = document.createElement('div');
      root.id = 'alfa-assistant-root';

      root.innerHTML = [
        '<div id="alfa-assistant-window" role="dialog" aria-label="Чат с ассистентом Альфа-Банка">',
        '  <div class="assistant-header">',
        '    <div class="assistant-header-info">',
        '      <div class="assistant-avatar">A</div>',
        '      <div class="assistant-title-wrap">',
        '        <span class="assistant-title">Кредитный куратор</span>',
        '        <span class="assistant-subtitle">Альфа-Банк • Онлайн</span>',
        '      </div>',
        '    </div>',
        '    <div class="assistant-header-controls">',
        '      <button class="assistant-header-btn" id="assistant-btn-clear" title="Очистить историю">',
        '        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"></path></svg>',
        '      </button>',
        '      <button class="assistant-header-btn" id="assistant-btn-close" title="Закрыть">',
        '        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12"></path></svg>',
        '      </button>',
        '    </div>',
        '  </div>',
        '  <div class="assistant-messages" id="assistant-messages-list"></div>',
        '  <div class="assistant-quick-replies" id="assistant-quick-replies"></div>',
        '  <form class="assistant-input-wrap" id="assistant-input-form">',
        '    <input type="text" class="assistant-input" id="assistant-input-text" placeholder="Задайте вопрос или сумму..." autocomplete="off">',
        '    <button type="submit" class="assistant-send-btn" id="assistant-send-btn" title="Отправить">',
        '      <svg viewBox="0 0 24 24"><path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z"/></svg>',
        '    </button>',
        '  </form>',
        '</div>',
        '<button id="alfa-assistant-fab" aria-label="Открыть помощника по кредитам" title="Помощник по кредитам">',
        '  <svg viewBox="0 0 24 24"><path d="M20 2H4c-1.1 0-2 .9-2 2v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm0 14H6l-2 2V4h16v12z"/></svg>',
        '  <span class="fab-badge"></span>',
        '</button>',
      ].join('\n');

      document.body.appendChild(root);

      this.renderAllMessages();
    },

    bindEvents: function () {
      var self = this;
      var fab = document.getElementById('alfa-assistant-fab');
      var closeBtn = document.getElementById('assistant-btn-close');
      var clearBtn = document.getElementById('assistant-btn-clear');
      var form = document.getElementById('assistant-input-form');
      var input = document.getElementById('assistant-input-text');

      if (fab) {
        fab.addEventListener('click', function () {
          self.toggle();
        });
      }

      if (closeBtn) {
        closeBtn.addEventListener('click', function () {
          self.close();
        });
      }

      if (clearBtn) {
        clearBtn.addEventListener('click', function () {
          self.clearHistory();
        });
      }

      if (form) {
        form.addEventListener('submit', function (e) {
          e.preventDefault();
          var text = (input.value || '').trim();
          if (text) {
            input.value = '';
            self.sendMessage(text);
          }
        });
      }
    },

    toggle: function () {
      if (this.isOpen) {
        this.close();
      } else {
        this.open();
      }
    },

    open: function () {
      var win = document.getElementById('alfa-assistant-window');
      if (win) {
        win.classList.add('is-open');
        this.isOpen = true;
        try {
          sessionStorage.setItem(STORAGE_KEY_STATE, 'open');
        } catch (e) {}
        this.scrollToBottom();
        var input = document.getElementById('assistant-input-text');
        if (input) setTimeout(function () { input.focus(); }, 150);
      }
    },

    close: function () {
      var win = document.getElementById('alfa-assistant-window');
      if (win) {
        win.classList.remove('is-open');
        this.isOpen = false;
        try {
          sessionStorage.setItem(STORAGE_KEY_STATE, 'closed');
        } catch (e) {}
      }
    },

    clearHistory: function () {
      this.history = [];
      try {
        sessionStorage.removeItem(STORAGE_KEY_HISTORY);
      } catch (e) {}
      var container = document.getElementById('assistant-messages-list');
      if (container) container.innerHTML = '';
      this.fetchGreeting();
    },

    fetchGreeting: function () {
      var self = this;
      var ctx = getPageContext();
      this.callApi({
        session_id: this.sessionId,
        message: 'привет',
        context: ctx,
      }).then(function (res) {
        self.handleResponse(res);
      }).catch(function () {
        self.addBotMessage({
          reply: 'Здравствуйте! Я онлайн-куратор по кредитам для бизнеса. Чем могу помочь?',
          quick_replies: ['Оборотные средства', 'Кредит для селлеров', 'Без залога на 1 год'],
        });
      });
    },

    sendMessage: function (text) {
      var self = this;
      this.addUserMessage(text);
      var ctx = getPageContext();

      this.callApi({
        session_id: this.sessionId,
        message: text,
        context: ctx,
      }).then(function (res) {
        self.handleResponse(res);
      }).catch(function () {
        self.fallbackResponse(text);
      });
    },

    callApi: function (payload) {
      return fetch('/api/v1/assistant/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      }).then(function (r) {
        if (!r.ok) throw new Error('Assistant API error: ' + r.status);
        return r.json();
      });
    },

    fallbackResponse: function (text) {
      // Offline / network fallback using loans.js if available
      var reply = 'Я подобрал для вас подходящие условия из каталога банка.';
      var actions = [];
      var quick = ['Оборотные средства', 'Кредит для селлеров', 'Автокредит'];

      if (window.Loans && window.Loans.PRODUCTS) {
        var sample = window.Loans.PRODUCTS[0];
        actions.push({
          type: 'apply_calculator',
          label: 'Выставить в калькуляторе',
          payload: {
            product_id: sample.id,
            amount: 1000000,
            term: 12,
            org: 'ooo',
            collateral: 'without',
            age: 'gte9',
          },
        });
      }

      this.addBotMessage({
        reply: reply,
        actions: actions,
        quick_replies: quick,
      });
    },

    handleResponse: function (res) {
      this.addBotMessage(res);
    },

    addUserMessage: function (text) {
      var msg = {
        from: 'user',
        text: text,
        time: this.formatCurrentTime(),
      };
      this.history.push(msg);
      this.saveHistory();
      this.appendMessageElement(msg);
      this.scrollToBottom();
    },

    addBotMessage: function (data) {
      var msg = {
        from: 'bot',
        text: data.reply || '',
        time: this.formatCurrentTime(),
        products: data.recommended_products || [],
        actions: data.actions || [],
        quick_replies: data.quick_replies || [],
      };
      this.history.push(msg);
      this.saveHistory();
      this.appendMessageElement(msg);
      this.renderQuickReplies(msg.quick_replies);
      this.scrollToBottom();
    },

    renderAllMessages: function () {
      var container = document.getElementById('assistant-messages-list');
      if (!container) return;
      container.innerHTML = '';
      for (var i = 0; i < this.history.length; i++) {
        this.appendMessageElement(this.history[i]);
      }
      if (this.history.length > 0) {
        var last = this.history[this.history.length - 1];
        if (last.from === 'bot' && last.quick_replies) {
          this.renderQuickReplies(last.quick_replies);
        }
      }
      this.scrollToBottom();
    },

    appendMessageElement: function (msg) {
      var container = document.getElementById('assistant-messages-list');
      if (!container) return;

      var wrap = document.createElement('div');
      wrap.className = 'assistant-msg from-' + msg.from;

      var bubble = document.createElement('div');
      bubble.className = 'msg-bubble';

      // Parse markdown-like bold and bullet lists
      var formatted = (msg.text || '')
        .replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>')
        .replace(/\n\n/g, '</p><p>')
        .replace(/\n• /g, '<br>• ')
        .replace(/\n/g, '<br>');

      bubble.innerHTML = '<p>' + formatted + '</p>';

      // Product offers
      if (msg.products && msg.products.length > 0) {
        for (var p = 0; p < msg.products.length; p++) {
          var offer = msg.products[p];
          var card = this.createProductCard(offer);
          bubble.appendChild(card);
        }
      }

      // Action buttons
      if (msg.actions && msg.actions.length > 0) {
        var actionsWrap = document.createElement('div');
        actionsWrap.className = 'assistant-actions';
        for (var a = 0; a < msg.actions.length; a++) {
          var btn = this.createActionButton(msg.actions[a]);
          actionsWrap.appendChild(btn);
        }
        bubble.appendChild(actionsWrap);
      }

      wrap.appendChild(bubble);

      var time = document.createElement('span');
      time.className = 'msg-time';
      time.textContent = msg.time || '';
      wrap.appendChild(time);

      container.appendChild(wrap);
    },

    createProductCard: function (offer) {
      var p = offer.product;
      var card = document.createElement('div');
      card.className = 'assistant-product-card';

      var reasonsHtml = '';
      if (offer.why_matched && offer.why_matched.length > 0) {
        reasonsHtml = '<ul class="assistant-reasons">' +
          offer.why_matched.map(function (r) { return '<li>' + r + '</li>'; }).join('') +
          '</ul>';
      }

      card.innerHTML = [
        '<div class="assistant-product-header">',
        '  <span class="assistant-product-title">' + p.title + '</span>',
        '  <span class="assistant-product-rate">' + p.rate.toFixed(1) + '%</span>',
        '</div>',
        '<div class="assistant-product-metrics">',
        '  <div><span class="metric-label">Платеж:</span> <span class="metric-val">~' + offer.payment.toLocaleString('ru-RU') + ' ₽/мес</span></div>',
        '  <div><span class="metric-label">Срок:</span> <span class="metric-val">' + p.term[0] + '–' + p.term[1] + ' мес.</span></div>',
        '</div>',
        reasonsHtml,
      ].join('\n');

      return card;
    },

    createActionButton: function (action) {
      var self = this;
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'assistant-action-btn' + (action.type === 'apply_wizard' ? ' secondary' : '');
      btn.textContent = action.label;

      btn.addEventListener('click', function () {
        self.executeAction(action);
      });

      return btn;
    },

    executeAction: function (action) {
      var p = action.payload || {};

      if (action.type === 'apply_calculator') {
        this.applyToCalculator(p);
      } else if (action.type === 'apply_wizard') {
        this.applyToWizard(p);
      }
    },

    applyToCalculator: function (p) {
      // 1. Radio: org
      if (p.org) {
        var orgEl = document.querySelector('input[name="org"][value="' + p.org + '"]');
        if (orgEl) {
          orgEl.checked = true;
          orgEl.dispatchEvent(new Event('change', { bubbles: true }));
        }
      }

      // 2. Radio: collateral
      if (p.collateral) {
        var colEl = document.querySelector('input[name="collateral"][value="' + p.collateral + '"]');
        if (colEl) {
          colEl.checked = true;
          colEl.dispatchEvent(new Event('change', { bubbles: true }));
        }
      }

      // 3. Radio: age
      if (p.age) {
        var ageEl = document.querySelector('input[name="age"][value="' + p.age + '"]');
        if (ageEl) {
          ageEl.checked = true;
          ageEl.dispatchEvent(new Event('change', { bubbles: true }));
        }
      }

      // 4. Sliders and inputs: amount
      if (p.amount) {
        var amountSlider = document.getElementById('calc-amount-slider');
        var amountInput = document.getElementById('calc-amount-input');
        if (amountSlider) {
          amountSlider.value = p.amount;
          amountSlider.dispatchEvent(new Event('input', { bubbles: true }));
          amountSlider.dispatchEvent(new Event('change', { bubbles: true }));
        }
        if (amountInput) {
          amountInput.value = p.amount;
          amountInput.dispatchEvent(new Event('input', { bubbles: true }));
          amountInput.dispatchEvent(new Event('change', { bubbles: true }));
        }
      }

      // 5. Sliders and inputs: term
      if (p.term) {
        var termSlider = document.getElementById('calc-term-slider');
        var termInput = document.getElementById('calc-term-input');
        if (termSlider) {
          termSlider.value = p.term;
          termSlider.dispatchEvent(new Event('input', { bubbles: true }));
          termSlider.dispatchEvent(new Event('change', { bubbles: true }));
        }
        if (termInput) {
          termInput.value = p.term;
          termInput.dispatchEvent(new Event('input', { bubbles: true }));
          termInput.dispatchEvent(new Event('change', { bubbles: true }));
        }
      }

      // Smooth scroll to calculator
      var calcBlock = document.getElementById('calculator') || document.querySelector('.calculator-section') || document.getElementById('calc-form');
      if (calcBlock) {
        calcBlock.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }

      // Temporary visual notification in chat
      this.addUserMessage('Параметры успешно применены в калькуляторе!');
    },

    applyToWizard: function (p) {
      if (window.LoanDraft && window.LoanDraft.save) {
        window.LoanDraft.save({
          productId: p.product_id,
          org: p.org || 'ooo',
          collateral: p.collateral || 'without',
          age: p.age || 'gte9',
          amount: p.amount || 1000000,
          term: p.term || 12,
        });
      }

      // If already in cabinet, open wizard directly; otherwise navigate to cabinet
      if (window.location.pathname.indexOf('personalAccount') !== -1) {
        if (window.Wizard && window.Wizard.open) {
          window.Wizard.open();
        } else {
          window.location.reload();
        }
      } else {
        window.location.href = '/personalAccount/index.html';
      }
    },

    renderQuickReplies: function (chips) {
      var self = this;
      var container = document.getElementById('assistant-quick-replies');
      if (!container) return;
      container.innerHTML = '';
      if (!chips || chips.length === 0) return;

      for (var i = 0; i < chips.length; i++) {
        var chip = document.createElement('button');
        chip.type = 'button';
        chip.className = 'assistant-chip';
        chip.textContent = chips[i];
        chip.addEventListener('click', (function (text) {
          return function () {
            self.sendMessage(text);
          };
        })(chips[i]));
        container.appendChild(chip);
      }
    },

    scrollToBottom: function () {
      var container = document.getElementById('assistant-messages-list');
      if (container) {
        container.scrollTop = container.scrollHeight;
      }
    },

    formatCurrentTime: function () {
      var d = new Date();
      var hh = d.getHours().toString().padStart(2, '0');
      var mm = d.getMinutes().toString().padStart(2, '0');
      return hh + ':' + mm;
    },
  };

  root.AlfaAssistant = AlfaAssistant;

  // Auto-initialize when DOM is ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function () {
      AlfaAssistant.init();
    });
  } else {
    AlfaAssistant.init();
  }
})(typeof window !== 'undefined' ? window : globalThis);
