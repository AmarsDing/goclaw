/** Client-side validation for skill ZIP files before upload.
 * Mirrors server-side checks in internal/http/skills_upload.go and skills_upload_layout.go */
import JSZip from "jszip";

export interface SkillZipValidation {
  valid: boolean;
  /** true when ZIP contains multiple top-level skill folders */
  multi?: boolean;
  skillCount?: number;
  name?: string;
  slug?: string;
  description?: string;
  /** i18n key under "upload." namespace */
  error?: string;
  errorDetail?: string;
}

const MAX_SKILL_SIZE = 20 * 1024 * 1024; // 20MB
const SLUG_REGEX = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;
const FRONTMATTER_REGEX = /^---\r?\n([\s\S]*?)\r?\n---/;

function normalizeZipPath(p: string): string {
  return p.replace(/\\/g, "/").replace(/^\.\//, "").trim();
}

function zipBasename(p: string): string {
  const parts = normalizeZipPath(p).split("/").filter(Boolean);
  return parts[parts.length - 1] ?? "";
}

type SkillEntry = { path: string; parts: string[] };

function collectSkillEntries(zip: JSZip): SkillEntry[] {
  const out: SkillEntry[] = [];
  for (const key of Object.keys(zip.files)) {
    const f = zip.files[key];
    if (!f || f.dir) continue;
    const n = normalizeZipPath(key);
    if (zipBasename(n) !== "SKILL.md") continue;
    const parts = n.split("/").filter((s) => s.length > 0);
    out.push({ path: n, parts });
  }
  return out;
}

/** Validate a skill ZIP file client-side. JSZip is lazy-loaded. */
export async function validateSkillZip(file: File): Promise<SkillZipValidation> {
  if (!file.name.toLowerCase().endsWith(".zip")) {
    return { valid: false, error: "upload.onlyZip" };
  }
  if (file.size > MAX_SKILL_SIZE) {
    return { valid: false, error: "upload.tooLarge" };
  }

  let zip: JSZip;
  try {
    zip = await JSZip.loadAsync(file);
  } catch {
    return { valid: false, error: "upload.invalidZip" };
  }

  const entries = collectSkillEntries(zip);
  if (entries.some((e) => e.parts.length > 3)) {
    return { valid: false, error: "upload.skillMdTooDeep" };
  }
  if (entries.length === 0) {
    return { valid: false, error: "upload.noSkillMd" };
  }

  const rootSkill = entries.find((e) => e.parts.length === 1 && e.parts[0] === "SKILL.md");
  if (rootSkill) {
    if (entries.length > 1) {
      return { valid: false, error: "upload.layoutMixedRoot" };
    }
    return validateSingleSkillMd(zip, "SKILL.md");
  }

  const depth2 = entries.filter((e) => e.parts.length === 2 && e.parts[1] === "SKILL.md");
  const depth3 = entries.filter((e) => e.parts.length === 3 && e.parts[2] === "SKILL.md");
  if (depth2.length > 0 && depth3.length > 0) {
    return { valid: false, error: "upload.layoutMixedDepth" };
  }

  if (depth2.length > 0) {
    const dirs = [...new Set(depth2.map((e) => e.parts[0]))].sort();
    const roots = dirs.map((d) => `${d}/SKILL.md`);
    if (dirs.length >= 2) {
      return validateMultiBundle(zip, roots);
    }
    return validateSingleSkillMd(zip, roots[0]!);
  }

  if (depth3.length === 0) {
    return { valid: false, error: "upload.noSkillMd" };
  }

  const wraps = [...new Set(depth3.map((e) => e.parts[0]))];
  if (wraps.length > 1) {
    return { valid: false, error: "upload.layoutMultipleWrappers" };
  }
  const w = wraps[0]!;
  const children = [...new Set(depth3.filter((e) => e.parts[0] === w).map((e) => e.parts[1]))].sort();
  const roots = children.map((c) => `${w}/${c}/SKILL.md`);
  if (children.length >= 2) {
    return validateMultiBundle(zip, roots);
  }
  return validateSingleSkillMd(zip, roots[0]!);
}

async function validateSingleSkillMd(zip: JSZip, key: string): Promise<SkillZipValidation> {
  const f = zip.files[key];
  if (!f || f.dir) {
    return { valid: false, error: "upload.noSkillMd" };
  }
  const skillMdContent = await f.async("string");
  if (!skillMdContent.trim()) {
    return { valid: false, error: "upload.emptySkillMd" };
  }
  const match = skillMdContent.match(FRONTMATTER_REGEX);
  if (!match?.[1]) {
    return { valid: false, error: "upload.noFrontmatter" };
  }
  const fields = parseFrontmatterFields(match[1]);
  if (!fields.name) {
    return { valid: false, error: "upload.nameRequired" };
  }
  const slug = fields.slug || slugify(fields.name);
  if (!SLUG_REGEX.test(slug)) {
    return { valid: false, error: "upload.invalidSlug", errorDetail: slug };
  }
  return {
    valid: true,
    name: fields.name,
    slug,
    description: fields.description,
  };
}

async function validateMultiBundle(zip: JSZip, skillPaths: string[]): Promise<SkillZipValidation> {
  const seen = new Set<string>();
  let firstName: string | undefined;
  for (const key of skillPaths) {
    const v = await validateSingleSkillMd(zip, key);
    if (!v.valid || !v.slug) {
      return v;
    }
    if (seen.has(v.slug)) {
      return { valid: false, error: "upload.duplicateSlugInBundle", errorDetail: v.slug };
    }
    seen.add(v.slug);
    if (!firstName) firstName = v.name;
  }
  return {
    valid: true,
    multi: true,
    skillCount: skillPaths.length,
    name: firstName,
  };
}

function parseFrontmatterFields(raw: string): Record<string, string> {
  const fields: Record<string, string> = {};
  for (const line of raw.split(/\r?\n/)) {
    const idx = line.indexOf(":");
    if (idx > 0) {
      const key = line.slice(0, idx).trim();
      const val = line
        .slice(idx + 1)
        .trim()
        .replace(/^["']|["']$/g, "");
      if (key && val) fields[key] = val;
    }
  }
  return fields;
}

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}
