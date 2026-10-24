from __future__ import annotations
from typing import Any, List, Optional, Dict

try:
    from pydantic import BaseModel, Field
except ImportError:
    class BaseModel:  # type: ignore
        def __init__(self, **kwargs):
            for k, v in kwargs.items():
                setattr(self, k, v)
        def model_dump(self, *args, **kwargs):
            return {
                k: (v.model_dump() if hasattr(v, "model_dump") else v)
                for k, v in self.__dict__.items()
            }
        def dict(self, *args, **kwargs):
            return self.model_dump()

    def Field(default=None, default_factory=None, **kwargs):  # type: ignore
        if default_factory is not None:
            return default_factory()
        return default



class ProductSpec(BaseModel):
    id: str = Field(..., description="Frontend ID (e.g. working-capital)")
    backend_id: str = Field(..., description="Go Backend catalog ID (e.g. working_capital)")
    num: int = Field(..., ge=1, le=14)
    category: str
    icon: str = "briefcase"
    rate: float = Field(..., description="Annual rate percentage")
    title: str
    desc: str
    org_types: List[str] = Field(..., description="Supported forms: ['ooo', 'ip']")
    collateral: str = Field(..., description="'without', 'with', 'any'")
    needs9: bool = Field(..., description="True if requires business age >= 9 months")
    term: List[int] = Field(..., min_length=2, max_length=2, description="[min_term, max_term] in months")
    amount: List[int] = Field(..., min_length=2, max_length=2, description="[min_amount, max_amount] in rubles")
    young_max: Optional[int] = Field(default=None, description="Max amount in rubles if business age < 9 months")
    required_docs: List[str] = Field(default_factory=list)
    field_groups: List[str] = Field(default_factory=list)


class CalculatorState(BaseModel):
    org: Optional[str] = None
    collateral: Optional[str] = None
    age: Optional[str] = None
    amount: Optional[int] = None
    term: Optional[int] = None


class ClientContext(BaseModel):
    page: Optional[str] = None
    company_inn: Optional[str] = None
    org_type: Optional[str] = None
    business_age_months: Optional[int] = None
    calculator_state: Optional[CalculatorState] = None
    application_id: Optional[str] = None
    application_status: Optional[str] = None
    missing_fields: Optional[List[str]] = None
    missing_docs: Optional[List[str]] = None


class ActionCommand(BaseModel):
    type: str = Field(..., description="apply_calculator, apply_wizard, navigate, fill_form")
    label: str
    payload: Dict[str, Any] = Field(default_factory=dict)


class ProductOffer(BaseModel):
    product: ProductSpec
    payment: int = Field(..., description="Estimated monthly annuity payment")
    overpayment: int = Field(..., description="Total interest overpayment")
    why_matched: List[str] = Field(default_factory=list)
    warnings: List[str] = Field(default_factory=list)


class ChatRequest(BaseModel):
    session_id: str = Field(default="default-session")
    message: str
    context: Optional[ClientContext] = None


class ChatResponse(BaseModel):
    session_id: str
    reply: str
    recommended_products: List[ProductOffer] = Field(default_factory=list)
    actions: List[ActionCommand] = Field(default_factory=list)
    quick_replies: List[str] = Field(default_factory=list)
    dialogue_state: str = "IDLE"


class RecommendRequest(BaseModel):
    org: str = Field(..., description="'ooo' or 'ip'")
    collateral: str = Field(default="without", description="'without', 'with', or 'any'")
    age_months: int = Field(default=12, ge=0)
    amount: int = Field(..., gt=0)
    term_months: int = Field(..., gt=0)
    purpose: Optional[str] = None


class RecommendResponse(BaseModel):
    offers: List[ProductOffer]
    explanation: str
