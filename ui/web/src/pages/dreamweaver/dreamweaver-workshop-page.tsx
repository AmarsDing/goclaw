import { useState } from "react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeft, ArrowRight, Check, Sparkles } from "lucide-react";
import { PageHeader } from "@/components/shared/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { ROUTES } from "@/lib/constants";
import { cn } from "@/lib/utils";

const STEPS = 4 as const;

export function DreamweaverWorkshopPage() {
  const { t } = useTranslation("dreamweaver");
  const [step, setStep] = useState(1);
  const [goal, setGoal] = useState<"skill" | "agent" | "publish">("skill");

  const next = () => setStep((s) => Math.min(STEPS, s + 1));
  const back = () => setStep((s) => Math.max(1, s - 1));

  return (
    <div className="space-y-6 p-4 sm:p-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex items-start gap-3">
          <Sparkles className="mt-1 h-8 w-8 shrink-0 text-primary" aria-hidden />
          <PageHeader title={t("wizard.title")} description={t("wizard.subtitle")} />
        </div>
        <Button asChild variant="ghost" size="sm">
          <Link to={ROUTES.DREAMWEAVER}>
            <ArrowLeft className="mr-2 h-4 w-4" />
            {t("wizard.backToHub")}
          </Link>
        </Button>
      </div>

      <div className="flex gap-2">
        {Array.from({ length: STEPS }, (_, i) => i + 1).map((n) => (
          <div key={n} className="flex flex-1 items-center gap-2">
            <div
              className={cn(
                "flex h-9 w-9 shrink-0 items-center justify-center rounded-full border text-sm font-medium",
                step >= n ? "border-primary bg-primary/10 text-primary" : "border-muted text-muted-foreground",
              )}
            >
              {step > n ? <Check className="h-4 w-4" /> : n}
            </div>
            {n < STEPS && <div className={cn("h-px flex-1", step > n ? "bg-primary/50" : "bg-border")} />}
          </div>
        ))}
      </div>
      <p className="text-xs text-muted-foreground">
        {t("wizard.stepLabel", { current: step, total: STEPS })} — {t(`wizard.steps.s${step}.label`)}
      </p>

      <Card>
        <CardHeader>
          <CardTitle>{t(`wizard.steps.s${step}.title`)}</CardTitle>
          <CardDescription>{t(`wizard.steps.s${step}.desc`)}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {step === 1 && (
            <RadioGroup
              value={goal}
              onValueChange={(v) => setGoal(v as typeof goal)}
              className="grid gap-3 sm:grid-cols-3"
            >
              {(
                [
                  ["skill", t("wizard.goals.skill")],
                  ["agent", t("wizard.goals.agent")],
                  ["publish", t("wizard.goals.publish")],
                ] as const
              ).map(([value, label]) => (
                <Label
                  key={value}
                  htmlFor={`goal-${value}`}
                  className={cn(
                    "flex cursor-pointer items-center gap-3 rounded-lg border p-4 hover:bg-muted/50",
                    goal === value && "border-primary ring-1 ring-primary/20",
                  )}
                >
                  <RadioGroupItem value={value} id={`goal-${value}`} />
                  <span className="text-sm font-medium">{label}</span>
                </Label>
              ))}
            </RadioGroup>
          )}

          {step === 2 && (
            <ul className="list-inside list-disc space-y-2 text-sm text-muted-foreground">
              <li>{t("wizard.prepare.cli")}</li>
              <li>{t("wizard.prepare.workspace")}</li>
              <li>{t("wizard.prepare.token")}</li>
            </ul>
          )}

          {step === 3 && (
            <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap">
              {goal === "skill" && (
                <Button asChild>
                  <Link to={ROUTES.SKILLS}>{t("wizard.actions.openSkills")}</Link>
                </Button>
              )}
              {goal === "agent" && (
                <Button asChild>
                  <Link to={ROUTES.AGENTS}>{t("wizard.actions.openAgents")}</Link>
                </Button>
              )}
              {(goal === "publish" || goal === "skill" || goal === "agent") && (
                <Button asChild variant="outline">
                  <Link to={ROUTES.MARKETPLACE}>{t("wizard.actions.openMarketplace")}</Link>
                </Button>
              )}
            </div>
          )}

          {step === 4 && (
            <div className="space-y-3 text-sm text-muted-foreground">
              <p>{t("wizard.done.hint")}</p>
              <div className="flex flex-wrap gap-2">
                <Button asChild variant="secondary" size="sm">
                  <Link to={ROUTES.MARKETPLACE}>{t("wizard.actions.openMarketplace")}</Link>
                </Button>
                <Button asChild variant="outline" size="sm">
                  <Link to={ROUTES.DREAMWEAVER}>{t("wizard.backToHub")}</Link>
                </Button>
              </div>
            </div>
          )}

          <div className="flex justify-between border-t pt-4">
            <Button type="button" variant="outline" onClick={back} disabled={step <= 1}>
              {t("wizard.prev")}
            </Button>
            {step < STEPS ? (
              <Button type="button" onClick={next}>
                {t("wizard.next")}
                <ArrowRight className="ml-2 h-4 w-4" />
              </Button>
            ) : (
              <Button asChild>
                <Link to={ROUTES.MARKETPLACE}>{t("wizard.done.cta")}</Link>
              </Button>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
