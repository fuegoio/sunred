import { source } from "@/lib/source";
import { getMDXComponents } from "@/components/mdx";
import { DocsBody, DocsDescription, DocsPage, DocsTitle } from "fumadocs-ui/page";
import { notFound } from "next/navigation";

const ROOT = "product";

export default async function Page({ params }: { params: Promise<{ slug?: string[] }> }) {
  const { slug } = await params;
  const page = source.getPage(slug ? [ROOT, ...slug] : [ROOT]);
  if (!page || page.type === "openapi") notFound();

  const MDXContent = page.data.body;

  return (
    <DocsPage toc={page.data.toc}>
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription>{page.data.description}</DocsDescription>
      <DocsBody>
        <MDXContent components={getMDXComponents()} />
      </DocsBody>
    </DocsPage>
  );
}

export async function generateStaticParams() {
  return source
    .getPages()
    .filter((p) => p.slugs[0] === ROOT)
    .map((p) => ({ slug: p.slugs.slice(1) }));
}

export async function generateMetadata({ params }: { params: Promise<{ slug?: string[] }> }) {
  const { slug } = await params;
  const page = source.getPage(slug ? [ROOT, ...slug] : [ROOT]);
  if (!page) return {};
  return {
    title: page.data.title,
    description: page.data.description,
  };
}
