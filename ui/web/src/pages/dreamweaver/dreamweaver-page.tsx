import { useMemo, useState } from "react";
import { Link, generatePath } from "react-router";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Sparkles, Terminal, Wrench } from "lucide-react";
import { PageHeader } from "@/components/shared/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useHttp } from "@/hooks/use-ws";
import { useV3Flags, type V3Flags } from "@/hooks/use-v3-flags";
import { ROUTES } from "@/lib/constants";
import { queryKeys } from "@/lib/query-keys";
import { Switch } from "@/components/ui/switch";

interface AgentRow {
  id: string;
  agent_key?: string;
  display_name?: string;
}

export function DreamweaverPage() {
  const { t } = useTranslation("dreamweaver");
  const http = useHttp();

  const { data: agents } = useQuery({
    queryKey: queryKeys.agents.all,
    queryFn: () => http.get<{ agents?: AgentRow[] }>("/v1/agents"),
    staleTime: 60_000,
  });

  const agentList = agents?.agents ?? [];
  const [agentId, setAgentId] = useState<string>("");
  const selected = useMemo(() => agentList.find((a) => a.id === agentId), [agentList, agentId]);

  const { flags, loading: flagsLoading, toggleFlag } = useV3Flags(agentId);

  return (
    <div className="space-y-6 p-4 sm:p-6">
      <div className="flex items-start gap-3">
        <Sparkles className="mt-1 h-8 w-8 shrink-0 text-primary" aria-hidden />
        <PageHeader title={t("title")} description={t("subtitle")} />
      </div>

      <Tabs defaultValue="bridge">
        <TabsList>
          <TabsTrigger value="bridge">{t("tabs.bridge")}</TabsTrigger>
          <TabsTrigger value="layers">{t("tabs.layers")}</TabsTrigger>
          <TabsTrigger value="workshop">{t("tabs.workshop")}</TabsTrigger>
          <TabsTrigger value="alchemy">{t("tabs.alchemy")}</TabsTrigger>
          <TabsTrigger value="spirit">{t("tabs.spirit")}</TabsTrigger>
        </TabsList>

        <TabsContent value="bridge" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle>{t("bridge.title")}</CardTitle>
              <CardDescription>{t("bridge.desc")}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3 text-sm text-muted-foreground">
              <p>{t("bridge.ws")}</p>
              <code className="block rounded-md bg-muted p-3 text-xs">wss://&lt;host&gt;:&lt;port&gt;/ws</code>
              <p>{t("bridge.sse")}</p>
              <code className="block rounded-md bg-muted p-3 text-xs">GET /v1/bridge/events</code>
              <p>{t("bridge.auth")}</p>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="layers" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle>{t("layers.title")}</CardTitle>
              <CardDescription>{t("layers.desc")}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-2 text-sm text-muted-foreground">
              <p>{t("layers.order")}</p>
              <ul className="list-inside list-disc space-y-1">
                <li>plugin</li>
                <li>user</li>
                <li>project ({t("layers.file")})</li>
                <li>flags</li>
                <li>policy</li>
              </ul>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="workshop" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle>{t("workshop.title")}</CardTitle>
              <CardDescription>{t("workshop.desc")}</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <Button asChild className="w-fit">
                <Link to={ROUTES.DREAMWEAVER_WORKSHOP}>{t("workshop.openWizard")}</Link>
              </Button>
              <div className="flex flex-wrap gap-2">
              <Button asChild variant="outline">
                <Link to={ROUTES.SKILLS}>
                  <Wrench className="mr-2 h-4 w-4" />
                  {t("workshop.skills")}
                </Link>
              </Button>
              <Button asChild variant="outline">
                <Link to={ROUTES.AGENTS}>
                  <Terminal className="mr-2 h-4 w-4" />
                  {t("workshop.agents")}
                </Link>
              </Button>
              <Button asChild variant="outline">
                <Link to={ROUTES.MARKETPLACE}>{t("workshop.marketplace")}</Link>
              </Button>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="alchemy" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle>{t("alchemy.title")}</CardTitle>
              <CardDescription>{t("alchemy.desc")}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3 text-sm text-muted-foreground">
              <p>{t("alchemy.cli")}</p>
              <Button asChild variant="secondary" size="sm">
                <Link to={ROUTES.PACKAGES}>{t("alchemy.packages")}</Link>
              </Button>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="spirit" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle>{t("spirit.title")}</CardTitle>
              <CardDescription>{t("spirit.desc")}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="dw-agent">{t("spirit.pickAgent")}</Label>
                <select
                  id="dw-agent"
                  className="w-full max-w-md rounded-md border bg-background px-3 py-2 text-sm"
                  value={agentId}
                  onChange={(e) => setAgentId(e.target.value)}
                >
                  <option value="">{t("spirit.placeholder")}</option>
                  {agentList.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.display_name || a.agent_key || a.id}
                    </option>
                  ))}
                </select>
              </div>

              {agentId ? (
                <div className="space-y-4">
                  {flagsLoading || !flags ? (
                    <p className="text-sm text-muted-foreground">{t("spirit.loading")}</p>
                  ) : (
                    <div className="space-y-3">
                      {(Object.keys(flags) as (keyof V3Flags)[]).map((key) => (
                        <div key={key} className="flex items-center justify-between gap-4 rounded-md border p-3">
                          <span className="font-mono text-xs">{key}</span>
                          <Switch
                            checked={!!flags[key]}
                            onCheckedChange={(v) => void toggleFlag(key, v)}
                          />
                        </div>
                      ))}
                    </div>
                  )}
                  <Button asChild variant="outline" size="sm">
                    <Link to={generatePath(ROUTES.AGENT_DETAIL, { id: selected?.id ?? agentId })}>
                      {t("spirit.openAgent")}
                    </Link>
                  </Button>
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">{t("spirit.hint")}</p>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
