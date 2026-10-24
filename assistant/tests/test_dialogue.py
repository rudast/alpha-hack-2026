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


if __name__ == "__main__":
    unittest.main()
