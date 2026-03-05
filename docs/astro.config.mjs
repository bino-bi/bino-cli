// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightThemeNova from 'starlight-theme-nova';
import starlightLlmsTxt from 'starlight-llms-txt';

// https://astro.build/config
export default defineConfig({
	site: "https://cli.bino.bi",
	integrations: [
		starlight({
			title: 'BinoBI CLI',
			components: {
				Footer: './src/components/Footer.astro',
			},
			plugins: [
				starlightLlmsTxt(),
				starlightThemeNova({
				nav: [
					{ label: 'CLI', href: '/cli' },
					{ label: 'Reference', href: '/reference' },
					{ label: 'Support', href: '/support' },
				]
			})],
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/bino-bi/bino-cli-releases' }],
			lastUpdated: true,
			sidebar: [
				{
					label: 'Why bino',
					autogenerate: { directory: 'why' },
				},
				{
					label: 'Getting started',
					items: [
						{ label: 'Installation', slug: 'getting-started/installation' },
						{ label: 'Key ideas', slug: 'getting-started/key-ideas' },
						{ label: 'Your first report', slug: 'getting-started/first-report' },
					],
				},
				{
					label: 'Concepts',
					autogenerate: { directory: 'concepts' },
				},
				{
					label: 'How-to guides',
					autogenerate: { directory: 'guides' },
				},
				{
					label: 'Recipes',
					autogenerate: { directory: 'recipes' },
				},
				{
					label: 'CLI',
					autogenerate: { directory: 'cli' },
				},
				{
					label: 'Reference',
					autogenerate: { directory: 'reference' },
				},
				{
					label: 'Appendix',
					autogenerate: { directory: 'appendix' },
				},
				{
					label: 'Support',
					slug: 'support',
				},
			],
		}),
	],
});
