/*
 * api.js — общий код для работы с бэкендом. Подключайте на каждой странице ДО её собственного скрипта:
 *   <script src="/assets/js/api.js"></script>
 *
 * Что здесь есть:
 *   Auth.save(session)   — сохранить токен после входа
 *   Auth.token()         — токен или null, если его нет или он истёк
 *   Auth.clear()         — выйти (бэкенд не имеет метода выхода, поэтому просто забываем токен)
 *   Auth.requireLogin()  — на закрытых страницах: если токена нет, отправляет на страницу входа (/login/)
 *   api(path, options)   — fetch к /api/v1 с токеном; при 401 сам выходит на страницу входа
 *   api.newKey()         — новый ключ для заголовка Idempotency-Key
 */
(function () {
  'use strict';

  var KEY = 'auth_session';
  var SESSION_TTL_MS = 8 * 60 * 60 * 1000; // сессия на бэкенде живёт 8 часов
  var LOGIN_URL = '/login/'; // отдельная страница входа

  var Auth = {
    save: function (session) {
      var data = {
        token: session.token,
        role: session.role,
        expiresAt: Date.now() + SESSION_TTL_MS,
      };
      try {
        localStorage.setItem(KEY, JSON.stringify(data));
      } catch (e) {
        /* приватный режим браузера: токен не сохранится, вход будет работать только до перезагрузки */
      }
    },

    token: function () {
      try {
        var raw = localStorage.getItem(KEY);
        if (!raw) return null;
        var data = JSON.parse(raw);
        if (!data.token || data.expiresAt < Date.now()) {
          Auth.clear();
          return null;
        }
        return data.token;
      } catch (e) {
        return null;
      }
    },

    clear: function () {
      try {
        localStorage.removeItem(KEY);
      } catch (e) {}
    },

    requireLogin: function () {
      if (!Auth.token()) {
        window.location.replace(LOGIN_URL);
        return false;
      }
      return true;
    },
  };

  /**
   * Запрос к API. path — например '/me/bootstrap' (префикс /api/v1 добавляется сам).
   * options.json — объект, который нужно отправить как JSON; options.auth === false — без токена.
   * options.idempotencyKey — ключ для заголовка Idempotency-Key. Его требуют изменяющие запросы
   *   по заявкам; при повторе того же действия (например, после сетевого сбоя) передавайте тот же ключ.
   * Возвращает { ok, status, data, headers }. Сетевые сбои превращаются в status 0.
   */
  async function api(path, options) {
    options = options || {};
    var headers = { Accept: 'application/json' };
    var init = { method: options.method || 'GET', headers: headers };

    if (options.json !== undefined) {
      headers['Content-Type'] = 'application/json';
      init.body = JSON.stringify(options.json);
    }

    if (options.idempotencyKey) headers['Idempotency-Key'] = options.idempotencyKey;

    if (options.auth !== false) {
      var token = Auth.token();
      if (token) headers.Authorization = 'Bearer ' + token;
    }

    var response;
    try {
      response = await fetch('/api/v1' + path, init);
    } catch (e) {
      return { ok: false, status: 0, data: {}, headers: new Headers() };
    }

    var data = {};
    try {
      data = await response.json();
    } catch (e) {
      /* тело пустое или не JSON — оставляем {} */
    }

    // Сессия истекла или токен недействителен на закрытой странице — возвращаем на вход.
    if (response.status === 401 && options.auth !== false) {
      Auth.clear();
      window.location.replace(LOGIN_URL);
    }

    return { ok: response.ok, status: response.status, data: data, headers: response.headers };
  }

  // Ключ идемпотентности: бэкенд принимает от 8 до 128 печатных символов.
  // crypto.randomUUID есть только на https и localhost, поэтому есть запасной вариант.
  api.newKey = function () {
    if (window.crypto && typeof window.crypto.randomUUID === 'function') return window.crypto.randomUUID();
    return Date.now().toString(36) + '-' + Math.random().toString(36).slice(2) + Math.random().toString(36).slice(2);
  };

  window.Auth = Auth;
  window.api = api;
})();
