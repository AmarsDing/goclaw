import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import type { AgentData } from "@/types/agent";
import { readMarketplacePublished } from "@/pages/agents/agent-detail/agent-display-utils";

export interface AgentPublishPayload {
  name: string;
  description: string;
  version: string;
  license: string;
  author: string;
  force: boolean;
}

interface AgentPublishDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agent: AgentData | null;
  onConfirm: (payload: AgentPublishPayload) => Promise<void>;
}

export function AgentPublishDialog({ open, onOpenChange, agent, onConfirm }: AgentPublishDialogProps) {
  const { t } = useTranslation("agents");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [version, setVersion] = useState("1.0.0");
  const [license, setLicense] = useState("MIT");
  const [author, setAuthor] = useState("");
  const [force, setForce] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!agent || !open) return;
    setName(agent.display_name || agent.agent_key || "");
    setDescription(agent.agent_description ?? agent.frontmatter ?? "");
    setVersion("1.0.0");
    setLicense("MIT");
    setAuthor("");
    setForce(readMarketplacePublished(agent));
  }, [agent, open]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!agent || !name.trim() || !version.trim()) return;
    setSubmitting(true);
    try {
      await onConfirm({
        name: name.trim(),
        description: description.trim(),
        version: version.trim(),
        license: license.trim() || "MIT",
        author: author.trim(),
        force,
      });
      onOpenChange(false);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md" onClick={(e) => e.stopPropagation()}>
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>{t("publishDialog.title")}</DialogTitle>
            <DialogDescription>{t("publishDialog.description")}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3 py-2">
            <div className="space-y-1.5">
              <Label htmlFor="pub-name">{t("publishDialog.name")}</Label>
              <Input
                id="pub-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                autoComplete="off"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="pub-desc">{t("publishDialog.longDescription")}</Label>
              <Textarea
                id="pub-desc"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={3}
                className="resize-y min-h-[72px]"
              />
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div className="space-y-1.5">
                <Label htmlFor="pub-ver">{t("publishDialog.version")}</Label>
                <Input
                  id="pub-ver"
                  value={version}
                  onChange={(e) => setVersion(e.target.value)}
                  required
                  placeholder="1.0.0"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="pub-lic">{t("publishDialog.license")}</Label>
                <Input
                  id="pub-lic"
                  value={license}
                  onChange={(e) => setLicense(e.target.value)}
                  placeholder="MIT"
                />
              </div>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="pub-author">{t("publishDialog.author")}</Label>
              <Input
                id="pub-author"
                value={author}
                onChange={(e) => setAuthor(e.target.value)}
                placeholder={t("publishDialog.authorPlaceholder")}
              />
            </div>
            <div className="flex items-center justify-between gap-3 rounded-md border px-3 py-2">
              <span className="text-sm text-muted-foreground">{t("publishDialog.force")}</span>
              <Switch checked={force} onCheckedChange={(v) => setForce(!!v)} />
            </div>
          </div>
          <DialogFooter className="gap-2 sm:gap-0">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t("publishDialog.cancel")}
            </Button>
            <Button type="submit" disabled={submitting || !name.trim() || !version.trim()}>
              {submitting ? t("publishDialog.submitting") : t("publishDialog.confirm")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
