can we put the email block inside a modal [Didn't receive email?] two options: resend + your email might be blocked {guide to unblock} [Cancel] [Resend]

on the sign up page, can we have a "[Back arrow + Back]" ghost button below the logo do that on all the pages like 2fa as well instead of links at bottom.

also make 'forgot password' aligned under the password input, too much spacing at the moment between them. it should be like a description of the input.

also can we make in `plans/features` a plan for terms of service & privacy policy, env setting, reads from boilerplate .md files like privacy-policy.md, terms-of-service.md like /policy/terms-of-service, when enabled have page routes + checkout on sign up to agree.

also on the 2FA page on auth, the description below heading has the same spacing issue as password input, we need to make them closer togehter.
not sure if other pages have the same, take a look, its a subheading not a new section.

admin logout button not danger variant
2FA page (admin i was on) the copied shouldn't be raw text, it should be a tooltip that appears for 3 seconds and hides. on  `copy recovery codes` ensure rest of app enforces this. update docs.  

improve UI on events page, add some graphs, make filtering easier. allow selecting multiple events like a combobox to filter, with search and allow selecting an entire parent group that selects the children.  
- move date range into single dropdown 'last hour', 'today', 'yesterday', 'custom range' (date picker)
- maybe have a single `filters` icon button, and then open a modal and then in the ui after apply, show badges for each filter. cleanly you can just say like event groups of ['admin.read', 'user.read' + 12]
- we basically need this to be more professional and production ready. 

remove refresh buttons across the app, we dont need them, they can just do a page refresh.