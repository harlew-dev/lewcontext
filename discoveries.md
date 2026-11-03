getting started like tembo, is in sidebar shows items like - Create a project, Invite a member, create an API key, in like a `getting started progress` card bottom of sidebar? 


account locking feature when too many attempts / brute force attack. Unblockable via email.  
- But for the person attempting, they still recieve the same usual error message, but when successful they are blocked from logging in with 'This account has been blocked due to repeated failed login attempts. Check your email to unblock.'  so the end-user doesn't actually know when an account is blocked, and this feature can't be used to tell if an account is 'active' as if we returned it early, they would see the 'block' and then try again next day and see the 'not blocked' message, thats why we need to not inform the attacker its blocked.  
On the subsequent successful login attempts during an active blocked account, we should send an email to the user saying that a login has been successful but your account is blocked. If this way you [Unblock account], if it wasn't we recommend resetting your password [Reset Password] and enabling two-factor authentication.

  
Task/Notes
   - Planning agent, produces plan like we do now
   - Conversation, prompts you when has questions.
   - Confirm before build (can set to Auto)
   - New worktree
   - Reviewer agent
   - Security agent
   - Docs updater agent
   - Secure commit packing and push.
   - PR

- Structued plan writing for example
  - Overview - jot down your ideas
  - Features - what features do yo uneed
  - Future features - what you dont need right now.
  - Optional - nice to have
  - MVP - what would meet the MVP?
  - Security - any security notes you have
     its all a custom workflow xd