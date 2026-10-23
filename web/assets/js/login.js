/*
 * login.js — логика страницы входа клиента.
 *
 * Бэкенд (менять не нужно):
 *   POST /api/v1/auth/login   { role, login, password }      -> 200 { challenge_id }
 *   POST /api/v1/auth/verify  { challenge_id, code }         -> 200 { token, role, redirect }
 * Ошибки приходят как { "error": "..." }:
 *   401 invalid credentials | 401 invalid code | 401 invalid or expired challenge
 *   429 too many attempts (заголовок Retry-After, секунды) | 400 | 500
 */
(function () {
  'use strict';

  // ---------- Настройки ----------

  // Демо-режим: показывает кнопки быстрого входа и подсказку про код 000000.
  // Перед показом жюри поставьте false — блоки исчезнут.
  var DEMO_MODE = true;
  var DEMO_PASSWORD = 'demo-password';
  var DEMO_CODE = '000000';
  var DEMO_CLIENTS = [
    { label: 'ООО «Демо Торг»', login: '+79990000001' },
    { label: 'ООО «Демо Строй»', login: '+79990000002' },
    { label: 'ООО «Демо Логистик»', login: '+79990000003' },
    { label: 'ООО «Демо Долг»', login: '+79990000004' },
    { label: 'ИП Иванов Демо', login: '+79990000005' },
  ];

  var ROLE = 'client'; // на этой странице входят только клиенты
  var CODE_LENGTH = 6;
  var CHALLENGE_TTL_MS = 5 * 60 * 1000; // challenge_id живёт 5 минут
  var DEFAULT_REDIRECT = '/personalAccount';

  // ---------- Элементы ----------

  var $ = function (id) {
    return document.getElementById(id);
  };

  var viewLogin = $('view-login');
  var viewVerify = $('view-verify');
  var formLogin = $('form-login');
  var formVerify = $('form-verify');
  var fieldLogin = $('field-login');
  var fieldPassword = $('field-password');
  var loginControl = $('login-control');
  var passwordControl = $('password-control');
  var btnLogin = $('btn-login');
  var btnVerify = $('btn-verify');
  var otpInputs = Array.prototype.slice.call(document.querySelectorAll('.otp-input'));
  var otpTimer = $('otp-timer');

  // ---------- Состояние ----------

  var challengeId = '';
  var challengeDeadline = 0;
  var timerId = null;
  var busy = false;

  // ---------- Ошибки ----------

  function showError(message) {
    $('error-message').textContent = message;
    $('error-plate').classList.add('visible');
  }

  function clearError() {
    $('error-plate').classList.remove('visible');
    loginControl.classList.remove('input-error');
    passwordControl.classList.remove('input-error');
    otpInputs.forEach(function (el) {
      el.classList.remove('input-error');
    });
  }

  function serverErrorText(res) {
    if (res.status === 0) {
      return 'Не удалось связаться с сервером. Проверьте подключение и попробуйте ещё раз.';
    }
    if (res.status === 429) {
      var seconds = parseInt(res.headers.get('Retry-After') || '900', 10);
      var minutes = Math.max(1, Math.ceil(seconds / 60));
      return 'Слишком много попыток. Повторите вход через ' + minutes + ' мин.';
    }
    if (res.status === 400) {
      return 'Не удалось отправить данные. Обновите страницу и попробуйте ещё раз.';
    }
    return 'Сервис входа временно недоступен. Попробуйте чуть позже.';
  }

  // ---------- Переключение шагов ----------

  function showStep(step) {
    clearError();
    viewLogin.classList.toggle('hidden', step !== 'login');
    viewVerify.classList.toggle('hidden', step !== 'verify');
  }

  function setBusy(button, isBusy, idleText, busyText) {
    busy = isBusy;
    button.disabled = isBusy;
    button.innerHTML = isBusy
      ? '<div class="spinner" aria-hidden="true"></div><span>' + busyText + '</span>'
      : '<span>' + idleText + '</span>';
  }

  // ---------- Шаг 1: телефон или почта и пароль ----------

  async function submitLogin(event) {
    event.preventDefault();
    if (busy) return;
    clearError();

    var login = fieldLogin.value.trim();
    var password = fieldPassword.value;

    if (!login || !password) {
      if (!login) loginControl.classList.add('input-error');
      if (!password) passwordControl.classList.add('input-error');
      showError('Введите телефон или почту и пароль.');
      (login ? fieldPassword : fieldLogin).focus();
      return;
    }

    setBusy(btnLogin, true, 'Продолжить', 'Проверяем данные…');
    var res = await api('/auth/login', {
      method: 'POST',
      auth: false,
      json: { role: ROLE, login: login, password: password },
    });
    setBusy(btnLogin, false, 'Продолжить', '');

    if (!res.ok || !res.data.challenge_id) {
      if (res.status === 401) {
        loginControl.classList.add('input-error');
        passwordControl.classList.add('input-error');
        showError('Неверный телефон, почта или пароль. Проверьте данные и попробуйте ещё раз.');
      } else {
        showError(serverErrorText(res));
      }
      return;
    }

    challengeId = res.data.challenge_id;
    openVerifyStep(login);
  }

  function openVerifyStep(login) {
    $('verify-target').textContent = login;
    otpInputs.forEach(function (el) {
      el.value = '';
    });
    showStep('verify');
    startTimer();
    otpInputs[0].focus();
  }

  function backToLogin(message, resetPassword) {
    stopTimer();
    challengeId = '';
    if (resetPassword) fieldPassword.value = '';
    showStep('login');
    if (message) showError(message);
    (resetPassword ? fieldPassword : fieldLogin).focus();
  }

  // ---------- Таймер жизни кода ----------

  function startTimer() {
    stopTimer();
    challengeDeadline = Date.now() + CHALLENGE_TTL_MS;
    renderTimer();
    timerId = setInterval(function () {
      renderTimer();
      if (Date.now() >= challengeDeadline) {
        backToLogin('Время на ввод кода истекло. Войдите заново.', true);
      }
    }, 250);
  }

  function stopTimer() {
    if (timerId) {
      clearInterval(timerId);
      timerId = null;
    }
  }

  function renderTimer() {
    var left = Math.max(0, Math.ceil((challengeDeadline - Date.now()) / 1000));
    var mm = String(Math.floor(left / 60)).padStart(2, '0');
    var ss = String(left % 60).padStart(2, '0');
    otpTimer.textContent = mm + ':' + ss;
  }

  // ---------- Шаг 2: код подтверждения ----------

  function readCode() {
    return otpInputs
      .map(function (el) {
        return el.value;
      })
      .join('');
  }

  function fillCode(digits, startIndex) {
    for (var i = 0; i < digits.length && startIndex + i < otpInputs.length; i++) {
      otpInputs[startIndex + i].value = digits.charAt(i);
    }
    var next = Math.min(startIndex + digits.length, otpInputs.length - 1);
    otpInputs[next].focus();
    if (readCode().length === CODE_LENGTH) submitVerify();
  }

  otpInputs.forEach(function (input, index) {
    // Ввод цифры. Если браузер подставил сразу весь код (автозаполнение из SMS), раскладываем по ячейкам.
    input.addEventListener('input', function () {
      var digits = input.value.replace(/\D/g, '');
      input.value = '';
      if (digits) fillCode(digits, index);
    });

    input.addEventListener('keydown', function (e) {
      if (e.key === 'Backspace' && !input.value && index > 0) {
        otpInputs[index - 1].value = '';
        otpInputs[index - 1].focus();
        e.preventDefault();
      } else if (e.key === 'ArrowLeft' && index > 0) {
        otpInputs[index - 1].focus();
        e.preventDefault();
      } else if (e.key === 'ArrowRight' && index < otpInputs.length - 1) {
        otpInputs[index + 1].focus();
        e.preventDefault();
      }
    });

    input.addEventListener('paste', function (e) {
      e.preventDefault();
      var text = (e.clipboardData || window.clipboardData).getData('text') || '';
      var digits = text.replace(/\D/g, '').slice(0, CODE_LENGTH);
      if (digits) fillCode(digits, 0);
    });

    input.addEventListener('focus', function () {
      input.select();
    });
  });

  async function submitVerify(event) {
    if (event) event.preventDefault();
    if (busy) return;
    clearError();

    var code = readCode();
    if (code.length !== CODE_LENGTH) {
      showError('Введите все 6 цифр кода.');
      var firstEmpty = otpInputs.find(function (el) {
        return !el.value;
      });
      if (firstEmpty) firstEmpty.focus();
      return;
    }

    setBusy(btnVerify, true, 'Войти', 'Проверяем код…');
    var res = await api('/auth/verify', {
      method: 'POST',
      auth: false,
      json: { challenge_id: challengeId, code: code },
    });
    setBusy(btnVerify, false, 'Войти', '');

    if (!res.ok) {
      handleVerifyError(res);
      return;
    }

    // Страница только для клиентов: токен другой роли не принимаем.
    if (!res.data.token || res.data.role !== ROLE) {
      backToLogin('Для этой учётной записи вход в кабинет клиента недоступен.', true);
      return;
    }

    stopTimer();
    Auth.save({ token: res.data.token, role: res.data.role });
    window.location.href = safeRedirect(res.data.redirect);
  }

  function handleVerifyError(res) {
    var code = res.data && res.data.error;

    if (res.status === 401 && code === 'invalid code') {
      otpInputs.forEach(function (el) {
        el.value = '';
        el.classList.add('input-error');
      });
      showError('Неверный код. Проверьте цифры и попробуйте ещё раз.');
      otpInputs[0].focus();
      return;
    }

    if (res.status === 401) {
      // challenge устарел или аннулирован после трёх неверных кодов
      backToLogin('Попытка входа закончилась. Введите пароль и запросите код заново.', true);
      return;
    }

    showError(serverErrorText(res));
  }

  // Редирект приходит с сервера, но на всякий случай пускаем только пути внутри сайта.
  function safeRedirect(path) {
    if (typeof path === 'string' && path.charAt(0) === '/' && path.charAt(1) !== '/') {
      return path;
    }
    return DEFAULT_REDIRECT;
  }

  // ---------- Мелочи интерфейса ----------

  $('btn-toggle-password').addEventListener('click', function () {
    var hidden = fieldPassword.type === 'password';
    fieldPassword.type = hidden ? 'text' : 'password';
    this.setAttribute('aria-pressed', String(hidden));
    this.setAttribute('aria-label', hidden ? 'Скрыть пароль' : 'Показать пароль');
    $('icon-eye').classList.toggle('hidden', hidden);
    $('icon-eye-off').classList.toggle('hidden', !hidden);
  });

  [fieldLogin, fieldPassword].forEach(function (el) {
    el.addEventListener('input', clearError);
  });

  $('btn-back').addEventListener('click', function () {
    backToLogin('', false);
  });

  formLogin.addEventListener('submit', submitLogin);
  formVerify.addEventListener('submit', submitVerify);

  // ---------- Демо-режим ----------

  if (DEMO_MODE) {
    var pills = $('demo-login-pills');
    DEMO_CLIENTS.forEach(function (client) {
      var pill = document.createElement('button');
      pill.type = 'button';
      pill.className = 'demo-pill';
      pill.textContent = client.label;
      pill.addEventListener('click', function () {
        fieldLogin.value = client.login;
        fieldPassword.value = DEMO_PASSWORD;
        clearError();
        btnLogin.focus();
      });
      pills.appendChild(pill);
    });
    $('demo-login').classList.remove('hidden');
    $('demo-verify').classList.remove('hidden');
    $('demo-code-hint').classList.remove('hidden');
    $('btn-fill-code').addEventListener('click', function () {
      fillCode(DEMO_CODE, 0);
    });
  }
})();
