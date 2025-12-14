// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightThemeNova from 'starlight-theme-nova';

// https://astro.build/config
export default defineConfig({
	integrations: [
		starlight({
			title: 'BinoBI CLI',
			plugins: [ starlightThemeNova(/* options */)],
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/bino-bi/bino-cli-releases' }],
			sidebar: [
				{
					label: 'Getting started',
					items: [
						{ label: 'Installation', slug: 'getting-started/installation' },
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
			],
		}),
	],
});
