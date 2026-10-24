"""
Tests for Domain QA Knowledge Base and Semantic Retrieval Engine.
"""
import unittest
from assistant.domain.qa_knowledge_base import qa_engine, KNOWLEDGE_BASE, QASearchEngine


class TestQAKnowledgeBase(unittest.TestCase):
    def test_total_items_count(self):
        self.assertGreaterEqual(len(KNOWLEDGE_BASE), 20)

    def test_early_repayment_query(self):
        res = qa_engine.search("А можно ли погасить кредит досрочно без штрафов?", top_k=1)
        self.assertTrue(len(res) > 0)
        item, score = res[0]
        self.assertEqual(item.id, "early_repayment")
        self.assertIn("0 ₽", item.answer)

    def test_fees_query(self):
        res = qa_engine.search("Есть ли скрытые комиссии за открытие или выдачу?", top_k=1)
        self.assertTrue(len(res) > 0)
        item, score = res[0]
        self.assertEqual(item.id, "fees_and_hidden_costs")

    def test_speed_query(self):
        res = qa_engine.search("Сколько по времени рассматривают заявку на кредит?", top_k=1)
        self.assertTrue(len(res) > 0)
        item, score = res[0]
        self.assertEqual(item.id, "decision_speed")

    def test_collateral_query(self):
        res = qa_engine.search("Какое имущество принимается в залог?", top_k=1)
        self.assertTrue(len(res) > 0)
        item, score = res[0]
        self.assertEqual(item.id, "collateral_requirements")

    def test_young_business_query(self):
        res = qa_engine.search("Мы работаем меньше 9 месяцев, какой кредит дадут?", top_k=1)
        self.assertTrue(len(res) > 0)
        item, score = res[0]
        self.assertEqual(item.id, "business_age_under_9m")

    def test_overdraft_vs_vkl_query(self):
        res = qa_engine.search("Чем овердрафт отличается от кредитной линии ВКЛ?", top_k=1)
        self.assertTrue(len(res) > 0)
        item, score = res[0]
        self.assertEqual(item.id, "overdraft_vs_credit_line")

    def test_signing_query(self):
        res = qa_engine.search("Как происходит подписание договора по смс?", top_k=1)
        self.assertTrue(len(res) > 0)
        item, score = res[0]
        self.assertEqual(item.id, "signing_process")

    def test_rejection_query(self):
        res = qa_engine.search("Почему могут отказать в кредите и что делать?", top_k=1)
        self.assertTrue(len(res) > 0)
        item, score = res[0]
        self.assertEqual(item.id, "rejection_reasons")

    def test_capabilities_guide_query_and_get_by_id(self):
        item = qa_engine.get_by_id("assistant_capabilities_guide")
        self.assertIsNotNone(item)
        self.assertEqual(item.category, "capabilities")
        res = qa_engine.search("Что ты умеешь и что можно написать?", top_k=1)
        self.assertTrue(len(res) > 0)
        self.assertEqual(res[0][0].id, "assistant_capabilities_guide")


if __name__ == "__main__":
    unittest.main()
