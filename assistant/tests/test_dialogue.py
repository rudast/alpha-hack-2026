import unittest
from assistant.domain.models import ChatRequest, ClientContext, CalculatorState
from assistant.engine.dialogue_manager import DialogueManager


class TestDialogueManager(unittest.TestCase):
    def setUp(self):
        self.dm = DialogueManager()

    def test_greeting(self):
        req = ChatRequest(session_id="test-1", message="Добрый день!")
        resp = self.dm.process(req)
        self.assertEqual(resp.dialogue_state, "GREETING")
        self.assertTrue(len(resp.quick_replies) > 0)

    def test_recommendation_and_actions(self):
        req = ChatRequest(
            session_id="test-2",
            message="Нужно 3 миллиона на закупку товара для маркетплейса, бизнесу 5 месяцев",
        )
        resp = self.dm.process(req)
        self.assertEqual(resp.dialogue_state, "RECOMMENDATION")
        self.assertTrue(len(resp.recommended_products) > 0)
        self.assertEqual(resp.recommended_products[0].product.id, "seller")

        # Action commands must be generated
        action_types = [a.type for a in resp.actions]
        self.assertIn("apply_calculator", action_types)
        self.assertIn("apply_wizard", action_types)

        # Check payload
        calc_action = next(a for a in resp.actions if a.type == "apply_calculator")
        self.assertEqual(calc_action.payload["product_id"], "seller")
        self.assertEqual(calc_action.payload["amount"], 3_000_000)
        self.assertEqual(calc_action.payload["term"], 18)

    def test_multi_turn_slot_preservation(self):
        session_id = "test-3"
        # Turn 1: specify amount and purpose
        req1 = ChatRequest(session_id=session_id, message="Хочу кредит 1 млн на оборотку")
        resp1 = self.dm.process(req1)
        self.assertEqual(resp1.recommended_products[0].product.id, "working-capital")

        # Turn 2: specify term
        req2 = ChatRequest(session_id=session_id, message="на 2 года")
        resp2 = self.dm.process(req2)
        # Check that amount 1m is still remembered and term is 24
        calc_action = next(a for a in resp2.actions if a.type == "apply_calculator")
        self.assertEqual(calc_action.payload["amount"], 1_000_000)
        self.assertEqual(calc_action.payload["term"], 24)

    def test_cabinet_info_requested_context(self):
        req = ChatRequest(
            session_id="test-4",
            message="Что с моей заявкой?",
            context=ClientContext(
                page="cabinet",
                application_status="INFO_REQUESTED",
                missing_docs=["Бухгалтерский баланс"],
                missing_fields=["applicant.position"],
            ),
        )
        resp = self.dm.process(req)
        self.assertEqual(resp.dialogue_state, "STATUS_HELP")
        self.assertIn("INFO_REQUESTED", resp.reply)
        self.assertIn("Бухгалтерский баланс", resp.reply)

    def test_docs_consultation(self):
        req = ChatRequest(
            session_id="test-5",
            message="Какие документы нужны для автокредита?",
        )
        resp = self.dm.process(req)
        self.assertEqual(resp.dialogue_state, "DOCS_CONSULTATION")
        self.assertIn("ПТС", resp.reply)

    def test_comparison_dialogue(self):
        req = ChatRequest(
            session_id="test-6",
            message="Чем отличается овердрафт от кредитной линии?",
        )
        resp = self.dm.process(req)
        self.assertEqual(resp.dialogue_state, "COMPARISON")
        self.assertIn("Овердрафт", resp.reply)
        self.assertIn("ВКЛ", resp.reply)
        self.assertTrue(len(resp.actions) >= 2)

    def test_young_business_overdraft_explanation(self):
        req = ChatRequest(
            session_id="test-7",
            message="Нужен овердрафт 500 000 руб, мы открылись 4 месяца назад",
        )
        resp = self.dm.process(req)
        self.assertEqual(resp.dialogue_state, "RECOMMENDATION")
        # Should explain restriction and recommend young-friendly cashflow product
        self.assertIn("9 месяцев", resp.reply)

    def test_faq_and_gratitude(self):
        # Signing FAQ
        res_sign = self.dm.process(ChatRequest(session_id="test-8", message="Как подписать договор?"))
        self.assertEqual(res_sign.dialogue_state, "FAQ")
        self.assertIn("электронной подписи", res_sign.reply)

        # Timeline FAQ
        res_time = self.dm.process(ChatRequest(session_id="test-9", message="Сколько рассматривают заявку?"))
        self.assertEqual(res_time.dialogue_state, "FAQ")
        self.assertIn("рассмотрения", res_time.reply)

        # Gratitude
        res_grat = self.dm.process(ChatRequest(session_id="test-10", message="Спасибо!"))
        self.assertEqual(res_grat.dialogue_state, "IDLE")

        # Collect slots
        res_ask = self.dm.process(ChatRequest(session_id="test-11", message="Помогите выбрать кредит"))
        self.assertEqual(res_ask.dialogue_state, "COLLECTING_SLOTS")

    def test_increase_amount_flow(self):
        # First get a recommendation for young business
        req1 = ChatRequest(
            session_id="test-12",
            message="Нужно 2 миллиона на экспресс, бизнесу 5 месяцев",
        )
        self.dm.process(req1)

        # Then click "Хочу сумму побольше"
        req2 = ChatRequest(session_id="test-12", message="Хочу сумму побольше")
        resp2 = self.dm.process(req2)
        self.assertEqual(resp2.dialogue_state, "INCREASE_AMOUNT")
        self.assertIn("Увеличение суммы", resp2.reply)
        self.assertIn("10 000 000 ₽", resp2.reply)
        self.assertIn("10 млн ₽", resp2.quick_replies)

    def test_change_term_flow(self):
        req1 = ChatRequest(
            session_id="test-13",
            message="Оборотные средства 1 млн руб",
        )
        self.dm.process(req1)

        # Click "Изменить срок"
        req2 = ChatRequest(session_id="test-13", message="Изменить срок")
        resp2 = self.dm.process(req2)
        self.assertEqual(resp2.dialogue_state, "CHANGE_TERM")
        self.assertIn("Расчет ежемесячного платежа", resp2.reply)
        self.assertTrue(len(resp2.actions) > 0)

    def test_reset_flow(self):
        req1 = ChatRequest(
            session_id="test-14",
            message="Автокредит 5 млн",
        )
        self.dm.process(req1)

        # Reset
        req2 = ChatRequest(session_id="test-14", message="Другой продукт")
        resp2 = self.dm.process(req2)
        self.assertEqual(resp2.dialogue_state, "GREETING")
        self.assertIn("Параметры сброшены", resp2.reply)

    def test_semantic_qa_search_in_dialogue(self):
        req = ChatRequest(
            session_id="test-15",
            message="А можно ли закрыть кредит досрочно без комиссии?",
        )
        resp = self.dm.process(req)
        self.assertEqual(resp.dialogue_state, "QA_EXPERT")
        self.assertIn("0 ₽", resp.reply)
        self.assertIn("досрочно", resp.reply.lower())

    def test_bot_capabilities_inquiry(self):
        req = ChatRequest(
            session_id="test-16",
            message="Что ты умеешь?",
        )
        resp = self.dm.process(req)
        self.assertEqual(resp.dialogue_state, "QA_EXPERT")
        self.assertIn("Альфи", resp.reply)
        self.assertIn("Подбор программы", resp.reply)


if __name__ == "__main__":
    unittest.main()

