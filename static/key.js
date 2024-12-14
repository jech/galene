'use strict';
let galeneKeys = {
    from : null,
    focusAgain : function() {
        if ( galeneKeys.from ) {
            galeneKeys.from.focus();
            //galeneKeys.from = null;
        }
        else {
           document.body.focus();
        }
    },
    leave : function (event) {
        if ( event.target.classList.contains('first') ) {
           if ( event.shiftKey ) {
               let cm = document.querySelector('.contextualMenu');
               if ( cm ) {
                  cm.remove();
                  setTimeout(galeneKeys.focusAgain, 20);
                  return;
               }
           }
        }
        if ( event.target.classList.contains('last') ) {
           if ( ! event.shiftKey ) {
                let cm = document.querySelector('.contextualMenu');
               if ( cm ) {
                  cm.remove();
                  setTimeout(galeneKeys.focusAgain, 20);
                  return;
               }
            }       
        }
        if ( event.key === 'Escape' )
           setTimeout(galeneKeys.focusAgain, 20);
    },
    setTabindexContextMenu : function (target) {
        let context = document.querySelectorAll('.contextualMenuItemTitle');
        if ( context && context.length) {
            context.forEach( c => {
                let btn = document.createElement('button');
                btn.classList.add('contextualMenuItemTitle');
                btn.classList.add('contextualJs');
                btn.textContent = c.textContent;
                btn.addEventListener('click', galeneKeys.menuClick);
                c.replaceWith(btn);
            });
            context = document.querySelectorAll('.contextualMenuItemTitle');
            context[0].classList.add('first');
            let last = context.length - 1;
            context[last].classList.add('last');
            galeneKeys.from = target;
            context[0].focus();
            let pos = target.getBoundingClientRect();

            let contextParent = document.querySelector('.contextualMenu');
            contextParent.style.top = pos.top+20+'px';
            // the following work only if the display with is enough we may have to correct this.
            let x = 180;
            let w = window.innerWidth;
            if ( x + 200 > window.innerWidth ) {
                x = w - 205;
            }
            contextParent.style.left = x+'px';
            
            let menu = document.querySelector('ul.contextualMenu');
        } else {
             // Enter pressed    
             galeneKeys.focusAgain();
        }
    },
    menuClick : function(e) {
        galeneKeys.focusAgain();
    },
    processKey : function (event) {
        let target = event.target;
        let key = event.key;
        switch(key) {
            case 'Tab':
                if ( document.activeElement.classList.contains('contextualMenuItemTitle') ) {
                    galeneKeys.leave(event);
                    return;
                }
                if ( document.activeElement.id === 'sideBarCollapse')
                    return;
            break;
            case 'Escape':
                let dialog = document.querySelector('dialog');
                if ( dialog ) {
                    let open = dialog.getAttribute('open');
                    if ( open == '' ) {
                        dialog.close(); 
                        galeneKeys.focusAgain();
                        return;
                    }
                }
                let cm = document.querySelector('.contextualMenu');
                 if ( cm ) {
                   cm.remove();
                   galeneKeys.leave(event);
                   return;
                }
                // if the setting are open close them
                let sidebar = document.querySelector('#sidebarnav[open]');
                if ( sidebar ) {
                    event.preventDefault();
                    sidebar.removeAttribute('open');
                    return;
                }
                // check if we are within the chat
                let active = document.activeElement;
                let chat = document.querySelector('#left:not(.invisible)');
                if ( chat ) {
                    event.preventDefault();
                    chat.classList.add('invisible');
                    let showChat = document.querySelector('#show-chat');
                    showChat.classList.remove('invisible');
                    return;
                }
                // finally close possibly the user list 
                let userList = document.querySelector('#left-sidebar:not(.active)');
                if ( userList ) {
                    event.preventDefault();
                    document.querySelector('#left-sidebar').classList.add('active');
                    return;
                }
            break;
            case 'Enter':
            case ' ':
                if ( target.classList.contains('volume-mute') ) {
                    let state = 
                    target.click();
                    if ( translate && translate.translateList ) {
                        translate.setVolumeAria();
                    }
                    return;
                }
            break;

        }
        switch(target.nodeName) {
            case 'INPUT':
                let type = target.getAttribute('type');
                if ( type && type === 'submit' ) {
                    return;
                }
            case 'TEXTAREA':
            case 'SELECT':
                return;
            break;
        }
        let usercont = null;
        switch(key) {
            case 'u':
                event.preventDefault();
                usercont = document.querySelector('#left-sidebar');
                if ( !usercont.classList.contains('active')) {
                    /* focus first user */
                    let user = document.querySelector('#users .user-p');
                    if ( user )
                        user.focus();
                } else {
                    usercont.classList.remove('active');
                }
            break;
            case 'r':
                /* raise hand */
                let me = document.querySelector('#left-sidebar #users .user-p');
                if ( me ) {
                   if (me.classList.contains('user-status-raisehand') )
                        me.classList.remove('user-status-raisehand');
                    else
                        me.classList.add('user-status-raisehand');
                }
            break;
            case 'm':
                let localMute = getSettings().localMute;
                localMute = !localMute;
                setLocalMute(localMute, true);
            break;
            case 'c':
                /* Chat */
                event.preventDefault();
                let chat = document.querySelector('#left:not(.invisible)');
                if (!chat) {
                    setVisibility('left', true);
                    setVisibility('show-chat', false);
                    resizePeers();
                    chat = document.querySelector('#left:not(.invisible) textarea');
                }
                if (chat)
                    chat.focus();
            break;
       }
    },
    userClick : function (e) {
        galeneKeys.from = e.target;
        setTimeout(galeneKeys.setTabindexContextMenu,20, e.target);
    },
    addClickListener : function (mutationList) {
        for (const mutation of mutationList) {
            if (mutation.type === "childList") {
                if (mutation && mutation.addedNodes) {
                    let idx = mutation.addedNodes.length -1;
                    if (idx >= 0) {
                        mutation.addedNodes[idx].addEventListener('click', galeneKeys.userClick);
                    }
                }
            }   
        }
    },
    focusInput : function() {
        let input = document.querySelector('#input');
        input.focus();
    },
    collapseSidebar : function(e) {
        let sidebar = document.querySelector('#left-sidebar.active');
        if ( !sidebar ) {
            // focus the first user-p button done but no outline!
            //let user = document.querySelector('.user-p');
            let userP = document.querySelector('.user-p');
            if (userP)
                userP.focus();
        }
    },
    setSr : function (sr, toast) {
        sr.textContent = toast.textContent;
    },
    resetSr : function(sr) {
        sr.textContent = '';
    },
    setSrText : function() {
        let toast = document.querySelector('.toastify');
        let sr = document.querySelector('#srSpeak');
        if ( toast ) {
            setTimeout(galeneKeys.setSr, 50, sr, toast);
            setTimeout(galeneKeys.resetSr, 4000, sr);
        }
    },
    init : function () {
        // add mutation oberver
        let toObserve = document.querySelector('#users');
        if ( toObserve ) {
            const observer = new MutationObserver(galeneKeys.addClickListener);
            observer.observe(toObserve, {childList:true, subtree:false});
        }
        const popup = new MutationObserver(galeneKeys.setSrText);
        popup.observe(document.body, {childList: true,subtree: false});

        // add event listener to the show-chat button
        let showChat = document.querySelector('#show-chat');
        if ( showChat ) {
            showChat.addEventListener('click',galeneKeys.focusInput);
        }
        let left = document.querySelector('#sidebarCollapse');
        if ( left ) {
            left.addEventListener('click', galeneKeys.collapseSidebar);
        }
    },
};
document.addEventListener('keydown', galeneKeys.processKey);
document.addEventListener('DOMContentLoaded', galeneKeys.init);

